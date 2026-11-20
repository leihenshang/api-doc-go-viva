// Package runner 顺序跑一批请求，把每条的结果交回去。
//
// 只做「跑 + 记账」：请求怎么发仍然由 send 决定，runner 不碰界面、不碰磁盘。
// 失败的定义：传输错误、状态码 >= 400，或任一断言不通过。
package runner

import (
	"context"
	"strings"
	"time"

	"github.com/leihenshang/api-doc-go-viva/internal/send"
)

// Case 是一条要跑的请求（带它自己的断言 / 提取规则）。
type Case struct {
	Name  string
	File  string
	Plan  send.Plan
	Rules Rules
}

// Result 是一条的结果。
type Result struct {
	Name     string
	File     string
	Method   string
	URL      string
	Status   int
	Duration time.Duration
	Err      string
	// Failures 是没通过的断言，每条一句「期望 vs 实际」。
	Failures []string
	// Extracted 是这条提取到的变量，已并入本次运行的变量袋。
	Extracted map[string]string
	Failed    bool
}

// Options 控制怎么跑。
type Options struct {
	// StopOnFailure 为真时，第一条失败就停，剩下的不再跑。
	StopOnFailure bool
	// Vars 是本次运行的变量袋：初值一般是环境变量，跑的过程中被提取结果覆盖，
	// 后面的请求就能用 {{token}}。Run 会复制它，不改调用方的 map。
	Vars map[string]string
}

// Run 顺序执行 cases，每跑完一条就调一次 report（可以为 nil）。
// context 取消后停止发下一条，已发出的仍会跑完。
func Run(ctx context.Context, cases []Case, options Options, report func(Result)) []Result {
	results := make([]Result, 0, len(cases))
	bag := make(map[string]string, len(options.Vars)+len(cases))
	for name, value := range options.Vars {
		bag[name] = value
	}
	for _, item := range cases {
		if err := ctx.Err(); err != nil {
			break
		}
		plan := item.Plan
		// 运行期提取的变量优先于环境变量：同名的以这条链上新取到的为准。
		plan.Vars = mergedVars(plan.Vars, bag)
		method := strings.ToUpper(strings.TrimSpace(plan.File.HTTP.Method))
		if method == "" {
			method = "GET"
		}
		out := send.Do(ctx, plan)
		extracted, failures := Evaluate(item.Rules, Observation{
			Status:   out.Status,
			Body:     out.Body,
			Headers:  out.Headers,
			Duration: out.Duration,
		})
		for name, value := range extracted {
			bag[name] = value
		}
		result := Result{
			Name:      item.Name,
			File:      item.File,
			Method:    method,
			URL:       out.URL,
			Status:    out.Status,
			Duration:  out.Duration,
			Err:       out.Err,
			Failures:  failures,
			Extracted: extracted,
			Failed:    out.Err != "" || out.Status >= 400 || len(failures) > 0,
		}
		results = append(results, result)
		if report != nil {
			report(result)
		}
		if options.StopOnFailure && result.Failed {
			break
		}
	}
	return results
}

// mergedVars 合并两份变量：later 覆盖 base，返回新 map（调用方的 map 不动）。
func mergedVars(base, later map[string]string) map[string]string {
	if len(later) == 0 {
		return base
	}
	out := make(map[string]string, len(base)+len(later))
	for name, value := range base {
		out[name] = value
	}
	for name, value := range later {
		out[name] = value
	}
	return out
}
