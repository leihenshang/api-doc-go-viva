// Package runner 顺序跑一批请求，把每条的结果交回去。
//
// 只做「跑 + 记账」：请求怎么发仍然由 send 决定，runner 不碰界面、不碰磁盘。
// 失败的定义很直白——传输错误，或状态码 >= 400。
package runner

import (
	"context"
	"strings"
	"time"

	"github.com/leihenshang/api-doc-go-viva/internal/send"
)

// Case 是一条要跑的请求。
type Case struct {
	Name string
	File string
	Plan send.Plan
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
	Failed   bool
}

// Options 控制怎么跑。
type Options struct {
	// StopOnFailure 为真时，第一条失败就停，剩下的不再跑。
	StopOnFailure bool
}

// Run 顺序执行 cases，每跑完一条就调一次 report（可以为 nil）。
// context 取消后停止发下一条，已发出的仍会跑完。
func Run(ctx context.Context, cases []Case, options Options, report func(Result)) []Result {
	results := make([]Result, 0, len(cases))
	for _, item := range cases {
		if err := ctx.Err(); err != nil {
			break
		}
		plan := item.Plan
		method := strings.ToUpper(strings.TrimSpace(plan.File.HTTP.Method))
		if method == "" {
			method = "GET"
		}
		out := send.Do(ctx, plan)
		result := Result{
			Name:     item.Name,
			File:     item.File,
			Method:   method,
			URL:      out.URL,
			Status:   out.Status,
			Duration: out.Duration,
			Err:      out.Err,
			Failed:   out.Err != "" || out.Status >= 400,
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
