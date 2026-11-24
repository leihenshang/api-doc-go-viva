package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/leihenshang/api-doc-go-viva/internal/runner"
	"github.com/leihenshang/api-doc-go-viva/internal/send"
)

// RunCollectionArgs 是 run_collection 的参数。
type RunCollectionArgs struct {
	Workspace string `json:"workspace,omitempty"`
	// Folder 只跑这个分组（相对路径，如「用户」）；空表示整个集合。
	Folder string `json:"folder,omitempty"`
	Env    string `json:"env,omitempty"`
	// StopOnFailure 第一条失败就停。
	StopOnFailure bool `json:"stopOnFailure,omitempty"`
	// TimeoutSec 是单条超时（秒），缺省 30，最长 300。
	TimeoutSec int `json:"timeoutSec,omitempty"`
	// MaxRequests 限制本次最多跑多少条（缺省 50，最长 200），防止一把跑上千条。
	MaxRequests int `json:"maxRequests,omitempty"`
}

// RunCollection 按集合顺序跑一批 HTTP 请求，带 extract / assert 规则。
//
// 顺序执行：前一条 extract 出来的变量会被后面的 {{name}} 用掉，这是链式依赖；
// 并发会让「先登录再取数」失效，所以这里不做并发。
func (s *Service) RunCollection(ctx context.Context, in RunCollectionArgs) (string, error) {
	ws, err := s.pick(in.Workspace)
	if err != nil {
		return "", err
	}
	col, err := explorer.Load(ws.Dir)
	if err != nil {
		return "", fmt.Errorf("读集合失败：%w", err)
	}
	vars, envName := loadEnvVars(ws, in.Env)

	timeout := 30 * time.Second
	if in.TimeoutSec > 0 {
		timeout = time.Duration(in.TimeoutSec) * time.Second
	}
	if timeout > 5*time.Minute {
		timeout = 5 * time.Minute
	}
	limit := 50
	if in.MaxRequests > 0 {
		limit = in.MaxRequests
	}
	if limit > 200 {
		limit = 200
	}

	folder := strings.Trim(strings.TrimSpace(in.Folder), "/")
	follow := true
	cases := make([]runner.Case, 0, len(col.Entries))
	for _, entry := range col.Entries {
		if entry.Kind != explorer.KindHTTP || entry.Payload == nil {
			continue
		}
		if folder != "" && !strings.HasPrefix(entry.File, folder+"/") {
			continue
		}
		rules, err := runner.RulesOf(entry.Payload)
		if err != nil {
			return "", fmt.Errorf("读「%s」的运行规则失败：%w", entry.Name, err)
		}
		cases = append(cases, runner.Case{
			Name: entry.Name,
			File: entry.File,
			Plan: send.Plan{
				File:            entry.Payload,
				Vars:            vars,
				Timeout:         timeout,
				MaxBody:         1 << 20,
				Dir:             col.Dir,
				FollowRedirects: &follow,
				Defaults:        explorer.LoadDefaults(col.Dir),
			},
			Rules: rules,
		})
	}
	if len(cases) == 0 {
		if folder != "" {
			return "", fmt.Errorf("分组「%s」里没有可跑的请求", folder)
		}
		return "", fmt.Errorf("集合里没有可跑的请求")
	}
	// 截断时明说：模型不该以为剩下的也跑过了。
	total := len(cases)
	if total > limit {
		cases = cases[:limit]
	}

	results := runner.Run(ctx, cases, runner.Options{
		StopOnFailure: in.StopOnFailure,
		Vars:          vars,
	}, nil)

	var b strings.Builder
	fmt.Fprintf(&b, "集合：%s · 环境：%s\n", col.Name, orDash(envName))
	if total > limit {
		fmt.Fprintf(&b, "共 %d 条，本次只跑前 %d 条（maxRequests）\n", total, limit)
	} else {
		fmt.Fprintf(&b, "共 %d 条，顺序执行\n", total)
	}
	b.WriteString(runner.Text(results))
	return b.String(), nil
}
