package runner

import (
	"encoding/xml"
	"strings"
	"testing"
	"time"
)

// TestJUnitReport 通过 / 断言失败 / 请求失败三种结果都要能进 JUnit。
func TestJUnitReport(t *testing.T) {
	results := []Result{
		{Name: "列表", File: "用户/列表.yml", Method: "GET", URL: "http://x/api/users", Status: 200, Duration: 12 * time.Millisecond},
		{Name: "详情", File: "用户/详情.yml", Method: "GET", URL: "http://x/api/u/1", Status: 200,
			Duration: 8 * time.Millisecond, Failed: true, Failures: []string{"state 期望 ok，实际 down"}},
		{Name: "创建", File: "用户/创建.yml", Method: "POST", URL: "http://x/api/users", Status: 500,
			Duration: 5 * time.Millisecond, Failed: true, Err: "status 500"},
		{Name: "登录", File: "登录.yml", Method: "POST", URL: "http://x/login", Status: 200,
			Duration: 3 * time.Millisecond, Extracted: map[string]string{"token": "t-1"}},
	}
	text := JUnit("演示集合", results)
	if !strings.HasPrefix(text, `<?xml`) {
		t.Fatalf("应带 XML 头：%s", text[:40])
	}

	var pack struct {
		XMLName  xml.Name `xml:"testsuites"`
		Tests    int      `xml:"tests,attr"`
		Failures int      `xml:"failures,attr"`
		Suites   []struct {
			Cases []struct {
				Name    string `xml:"name,attr"`
				Failure []struct {
					Message string `xml:"message,attr"`
					Body    string `xml:",chardata"`
				} `xml:"failure"`
				Out string `xml:"system-out"`
			} `xml:"testcase"`
		} `xml:"testsuite"`
	}
	if err := xml.Unmarshal([]byte(text), &pack); err != nil {
		t.Fatalf("生成的 XML 解析不了：%v\n%s", err, text)
	}
	if pack.Tests != 4 || pack.Failures != 2 {
		t.Fatalf("统计不对：tests=%d failures=%d", pack.Tests, pack.Failures)
	}
	cases := pack.Suites[0].Cases
	if len(cases) != 4 || cases[0].Name != "列表" {
		t.Fatalf("testcase 不对：%+v", cases)
	}
	if len(cases[1].Failure) != 1 || !strings.Contains(cases[1].Failure[0].Body, "期望 ok") {
		t.Fatalf("断言失败详情丢了：%+v", cases[1].Failure)
	}
	if len(cases[2].Failure) != 1 || !strings.Contains(cases[2].Failure[0].Body, "status 500") {
		t.Fatalf("请求失败详情丢了：%+v", cases[2].Failure)
	}
	if !strings.Contains(cases[3].Out, "token = t-1") {
		t.Fatalf("提取到的变量没进 system-out：%q", cases[3].Out)
	}
}

// TestTextReport 终端报告：每条一行，末尾汇总。
func TestTextReport(t *testing.T) {
	text := Text([]Result{
		{Name: "列表", Method: "GET", URL: "http://x/a", Status: 200, Duration: time.Millisecond},
		{Name: "创建", Method: "POST", URL: "http://x/b", Status: 200, Duration: time.Millisecond,
			Failed: true, Failures: []string{"code 期望 201，实际 200"}},
	})
	if !strings.Contains(text, "ok   GET") {
		t.Fatalf("通过的条目格式不对：%s", text)
	}
	if !strings.Contains(text, "FAIL POST") {
		t.Fatalf("失败的条目格式不对：%s", text)
	}
	if !strings.Contains(text, "2 条：1 通过，1 失败") {
		t.Fatalf("汇总不对：%s", text)
	}
}
