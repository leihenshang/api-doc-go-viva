package runner

import (
	"encoding/xml"
	"fmt"
	"strings"
)

// JUnit 把一次运行的结果写成 JUnit XML（CI 直接能吃）。
//
// 一条请求 = 一个 testcase；没通过的断言进 <failure>，跑挂的请求（网络错误、
// 状态码不符）也进 <failure>，并把状态码 / 耗时 / 提取到的变量放进 <system-out>，
// 便于在 CI 页面上直接看出发生了什么。
type junitSuites struct {
	XMLName  xml.Name     `xml:"testsuites"`
	Name     string       `xml:"name,attr"`
	Tests    int          `xml:"tests,attr"`
	Failures int          `xml:"failures,attr"`
	Time     string       `xml:"time,attr"`
	Suites   []junitSuite `xml:"testsuite"`
}

type junitSuite struct {
	Name     string      `xml:"name,attr"`
	Tests    int         `xml:"tests,attr"`
	Failures int         `xml:"failures,attr"`
	Cases    []junitCase `xml:"testcase"`
}

type junitCase struct {
	Name      string       `xml:"name,attr"`
	Classname string       `xml:"classname,attr,omitempty"`
	Time      string       `xml:"time,attr"`
	Failure   []junitFail  `xml:"failure,omitempty"`
	SystemOut *junitSysOut `xml:"system-out,omitempty"`
}

type junitFail struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr,omitempty"`
	Body    string `xml:",chardata"`
}

type junitSysOut struct {
	Body string `xml:",chardata"`
}

// JUnit 生成 XML 文本；name 是套件名（一般用集合名）。
func JUnit(name string, results []Result) string {
	suite := junitSuite{Name: name}
	total := 0.0
	for _, result := range results {
		item := junitCase{
			Name:      result.Name,
			Classname: result.File,
			Time:      fmt.Sprintf("%.3f", result.Duration.Seconds()),
		}
		total += result.Duration.Seconds()
		if result.Failed {
			suite.Failures++
			message := "断言未通过"
			detail := strings.Join(result.Failures, "\n")
			if result.Err != "" {
				message = "请求失败"
				if detail != "" {
					detail = result.Err + "\n" + detail
				} else {
					detail = result.Err
				}
			}
			item.Failure = []junitFail{{Message: message, Type: "assertion", Body: detail}}
		}
		if result.Status > 0 || result.Err != "" || len(result.Extracted) > 0 {
			var out strings.Builder
			fmt.Fprintf(&out, "status: %d\nurl: %s\nmethod: %s\nduration: %s\n",
				result.Status, result.URL, result.Method, result.Duration)
			for key, value := range result.Extracted {
				fmt.Fprintf(&out, "extracted %s = %s\n", key, value)
			}
			item.SystemOut = &junitSysOut{Body: out.String()}
		}
		suite.Cases = append(suite.Cases, item)
		total += 0
	}
	suite.Tests = len(results)
	pack := junitSuites{
		Name: name, Tests: suite.Tests, Failures: suite.Failures,
		Time: fmt.Sprintf("%.3f", total), Suites: []junitSuite{suite},
	}
	data, err := xml.MarshalIndent(pack, "", "  ")
	if err != nil {
		return ""
	}
	return xml.Header + string(data) + "\n"
}

// Text 是终端报告：每条一行，末尾一行汇总。
func Text(results []Result) string {
	var out strings.Builder
	failed := 0
	for _, result := range results {
		mark := "ok  "
		if result.Failed {
			mark = "FAIL"
			failed++
		}
		fmt.Fprintf(&out, "%s %-6s %-28s %3d  %6d ms  %s\n",
			mark, result.Method, clip(result.Name, 28), result.Status,
			result.Duration.Milliseconds(), clip(result.URL, 60))
		for _, failure := range result.Failures {
			fmt.Fprintf(&out, "     - %s\n", failure)
		}
		if result.Err != "" {
			fmt.Fprintf(&out, "     ! %s\n", result.Err)
		}
	}
	fmt.Fprintf(&out, "\n%d 条：%d 通过，%d 失败\n", len(results), len(results)-failed, failed)
	return out.String()
}

// clip 按 rune 截断，避免把长地址撑爆终端。
func clip(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit-1]) + "…"
}
