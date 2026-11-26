// apidocctl 是命令行入口：`run` 把集合里的请求在终端跑一遍，`list` 列出集合里有什么。
//
// 为什么要有：CI 里跑接口回归比打开界面合适；报告能直接出 JUnit / JSON，
// GitHub Actions / GitLab CI 都认，jq 也能直接吃。
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
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
	case "list":
		return listCommand(args[1:], stdout, stderr)
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
  apidocctl list --collection <集合目录> [选项]
  apidocctl run  --collection <集合目录> [选项]

list 选项：
  --collection <dir>   集合目录（必填）
  --filter <文本>      只列名称 / 文件 / 地址里含这段文字的请求
  --no-color           不带颜色（列表本身不着色，参数保留以兼容脚本）

run 选项：
  --collection <dir>   集合目录（必填）
  --env <name>         环境名（读 environments/<name>.yml，含 secrets）
  --filter <文本>      只跑名称或路径里含这段文字的请求
  --stop-on-failure    第一条失败就停
  --retry <次数>       单条失败后重试几次（默认 0；重试不改变量袋语义）
  --timeout <时长>     单条超时（默认 30s）
  --reporter <格式>    text（默认）/ junit / json
  --output <文件>      报告写到文件（默认打印到 stdout；写文件时不着色）
  --no-color           text 报告不带 ANSI 颜色

退出码：0 全部通过；1 有失败；2 用法或环境错误。
`)
}

// listCommand 列出集合里的请求：方法 / 名称 / 分组 / 地址，按分组分段打印。
func listCommand(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dir := flags.String("collection", "", "集合目录")
	filter := flags.String("filter", "", "只列名称 / 文件 / 地址里含这段文字的请求")
	_ = flags.Bool("no-color", false, "不带颜色（列表本身不着色）")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*dir) == "" {
		fmt.Fprintln(stderr, "缺少 --collection")
		return 2
	}
	col, err := loadCollection(*dir, stderr)
	if err != nil {
		return 2
	}

	requests := listedRequests(col, *filter)
	fmt.Fprintf(stdout, "集合：%s（%s）\n", col.Name, col.Dir)
	if len(requests) == 0 {
		fmt.Fprintln(stdout, "没有匹配的请求")
		return 0
	}
	// 按分组聚合，组内保持集合顺序（条目已按 seq 排好）。
	order := make([]string, 0, 8)
	grouped := map[string][]*explorer.Entry{}
	for _, entry := range requests {
		group := requestGroup(entry.File)
		if _, ok := grouped[group]; !ok {
			order = append(order, group)
		}
		grouped[group] = append(grouped[group], entry)
	}
	for _, group := range order {
		fmt.Fprintf(stdout, "分组：%s\n", group)
		for _, entry := range grouped[group] {
			fmt.Fprintf(stdout, "  %-6s %-28s %-40s %s\n",
				strings.ToUpper(entry.Payload.HTTP.Method), entry.Name, entry.File, entry.Payload.HTTP.URL)
		}
	}
	fmt.Fprintf(stdout, "\n%d 个请求\n", len(requests))
	return 0
}

// listedRequests 取出可列出的 HTTP 请求（文档与 gRPC 跳过），并按 --filter 过滤。
func listedRequests(col *explorer.Collection, filter string) []*explorer.Entry {
	needle := strings.ToLower(strings.TrimSpace(filter))
	out := make([]*explorer.Entry, 0, len(col.Entries))
	for _, entry := range col.Entries {
		if entry.Kind != explorer.KindHTTP || entry.Payload == nil {
			continue
		}
		if needle != "" &&
			!strings.Contains(strings.ToLower(entry.Name), needle) &&
			!strings.Contains(strings.ToLower(entry.File), needle) &&
			!strings.Contains(strings.ToLower(entry.Payload.HTTP.URL), needle) {
			continue
		}
		out = append(out, entry)
	}
	return out
}

// requestGroup 是请求所在分组（文件所在目录；根目录显示 /）。
func requestGroup(file string) string {
	dir := path.Dir(file)
	if dir == "." || dir == "" {
		return "/"
	}
	return dir
}

func runCommand(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dir := flags.String("collection", "", "集合目录")
	envName := flags.String("env", "", "环境名")
	filter := flags.String("filter", "", "只跑名称或路径里含这段文字的请求")
	stopOnFailure := flags.Bool("stop-on-failure", false, "第一条失败就停")
	retry := flags.Int("retry", 0, "单条失败后重试次数")
	timeout := flags.Duration("timeout", 30*time.Second, "单条超时")
	reporter := flags.String("reporter", "text", "报告格式：text / junit / json")
	output := flags.String("output", "", "报告写到文件")
	noColor := flags.Bool("no-color", false, "text 报告不带 ANSI 颜色")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*dir) == "" {
		fmt.Fprintln(stderr, "缺少 --collection")
		return 2
	}
	if *retry < 0 {
		fmt.Fprintln(stderr, "--retry 不能是负数")
		return 2
	}
	format := strings.ToLower(strings.TrimSpace(*reporter))
	if format != "text" && format != "junit" && format != "json" {
		fmt.Fprintf(stderr, "不认识的报告格式 %q（可选 text / junit / json）\n", *reporter)
		return 2
	}

	col, err := loadCollection(*dir, stderr)
	if err != nil {
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
		Retry:         *retry,
	}, nil)

	text := ""
	switch format {
	case "json":
		encoded, err := runner.JSON(col.Name, results)
		if err != nil {
			fmt.Fprintf(stderr, "%v\n", err)
			return 2
		}
		text = encoded
	case "junit":
		text = runner.JUnit(col.Name, results)
	default:
		text = runner.Text(results)
		// 只有直接打到终端才着色：管道 / 文件里出现 ANSI 转义会污染 grep 与日志。
		if strings.TrimSpace(*output) == "" && !*noColor && colorEnabled(stdout) {
			text = paintText(text)
		}
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

// loadCollection 统一「读集合失败就按用法错退出」这一步（list / run 共用）。
func loadCollection(dir string, stderr io.Writer) (*explorer.Collection, error) {
	col, err := explorer.Load(strings.TrimSpace(dir))
	if err != nil {
		fmt.Fprintf(stderr, "读集合失败：%v\n", err)
		return nil, err
	}
	return col, nil
}

// colorEnabled 判断要不要给报告上色：只有写到真终端才上，重定向与管道一律不上。
func colorEnabled(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// paintText 给逐条结果上色：通过绿、失败红，汇总行保持原样。
func paintText(text string) string {
	lines := strings.Split(text, "\n")
	for index, line := range lines {
		switch {
		case strings.HasPrefix(line, "ok  "):
			lines[index] = "\x1b[32m" + line + "\x1b[0m"
		case strings.HasPrefix(line, "FAIL"):
			lines[index] = "\x1b[31m" + line + "\x1b[0m"
		}
	}
	return strings.Join(lines, "\n")
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
