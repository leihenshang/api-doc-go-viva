// apidocctl 是命令行入口：现在只有 `run`，把集合里的请求在终端跑一遍。
//
// 为什么要有：CI 里跑接口回归比打开界面合适；报告能直接出 JUnit，
// GitHub Actions / GitLab CI 都认。
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/leihenshang/api-doc-go-viva/internal/runner"
	"github.com/leihenshang/api-doc-go-viva/internal/send"
)

func main() {
	os.Exit(Run(os.Args[1:], os.Stdout, os.Stderr))
}

// Run 是 main 的可测版本：返回进程退出码（0 全通过 / 1 有失败 / 2 用法或环境错）。
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	switch args[0] {
	case "run":
		return runCommand(args[1:], stdout, stderr)
	case "-h", "--help", "help":
		usage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "不认识的子命令 %q\n\n", args[0])
		usage(stderr)
		return 2
	}
}

func usage(out io.Writer) {
	fmt.Fprint(out, `apidocctl —— API-DOC-GO 集合的命令行工具

用法：
  apidocctl run --collection <集合目录> [选项]

选项：
  --collection <dir>   集合目录（必填）
  --env <name>         环境名（读 environments/<name>.yml，含 secrets）
  --filter <文本>      只跑名称或路径里含这段文字的请求
  --stop-on-failure    第一条失败就停
  --timeout <时长>     单条超时（默认 30s）
  --reporter <格式>    text（默认）或 junit
  --output <文件>      报告写到文件（默认打印到 stdout）

退出码：0 全部通过；1 有失败；2 用法或环境错误。
`)
}

func runCommand(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dir := flags.String("collection", "", "集合目录")
	envName := flags.String("env", "", "环境名")
	filter := flags.String("filter", "", "只跑名称或路径里含这段文字的请求")
	stopOnFailure := flags.Bool("stop-on-failure", false, "第一条失败就停")
	timeout := flags.Duration("timeout", 30*time.Second, "单条超时")
	reporter := flags.String("reporter", "text", "报告格式：text / junit")
	output := flags.String("output", "", "报告写到文件")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*dir) == "" {
		fmt.Fprintln(stderr, "缺少 --collection")
		return 2
	}

	col, err := explorer.Load(*dir)
	if err != nil {
		fmt.Fprintf(stderr, "读集合失败：%v\n", err)
		return 2
	}
	vars := map[string]string{}
	if name := strings.TrimSpace(*envName); name != "" {
		loaded, err := explorer.LoadEnv(col.Dir, name)
		if err != nil {
			fmt.Fprintf(stderr, "读环境 %s 失败：%v\n", name, err)
			return 2
		}
		vars = loaded
	}

	cases, err := buildCases(col, vars, *filter, *timeout)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 2
	}
	if len(cases) == 0 {
		fmt.Fprintln(stderr, "没有可跑的请求（--filter 是不是太窄了？）")
		return 2
	}

	results := runner.Run(context.Background(), cases, runner.Options{
		StopOnFailure: *stopOnFailure,
		Vars:          vars,
	}, nil)

	text := runner.Text(results)
	if strings.EqualFold(*reporter, "junit") {
		text = runner.JUnit(col.Name, results)
	}
	if target := strings.TrimSpace(*output); target != "" {
		if err := os.WriteFile(target, []byte(text), 0o644); err != nil {
			fmt.Fprintf(stderr, "写报告失败：%v\n", err)
			return 2
		}
	} else {
		fmt.Fprint(stdout, text)
	}

	for _, result := range results {
		if result.Failed {
			return 1
		}
	}
	return 0
}

// buildCases 把集合里的 HTTP 请求转成运行条目；文档、gRPC 与目录跳过。
func buildCases(col *explorer.Collection, vars map[string]string, filter string, timeout time.Duration) ([]runner.Case, error) {
	follow := true
	cases := make([]runner.Case, 0, len(col.Entries))
	needle := strings.ToLower(strings.TrimSpace(filter))
	for _, entry := range col.Entries {
		if entry.Kind != explorer.KindHTTP || entry.Payload == nil {
			continue
		}
		if needle != "" &&
			!strings.Contains(strings.ToLower(entry.Name), needle) &&
			!strings.Contains(strings.ToLower(entry.File), needle) {
			continue
		}
		rules, err := runner.RulesOf(entry.Payload)
		if err != nil {
			return nil, fmt.Errorf("读「%s」的运行规则失败：%w", entry.Name, err)
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
	return cases, nil
}
