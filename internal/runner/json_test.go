package runner

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// JSON 报告：字段名稳定（jq 可用）、耗时是毫秒、空错误不出来、转义正确。
func TestJSONReport(t *testing.T) {
	results := []Result{
		{
			Name:     `登录 "用户"`,
			File:     "用户/登录.yml",
			Method:   "POST",
			URL:      "http://example.invalid/login",
			Status:   200,
			Duration: 1234 * time.Millisecond,
			Extracted: map[string]string{
				"token": "t-1",
			},
		},
		{
			Name:     "故障",
			File:     "故障.yml",
			Method:   "GET",
			URL:      "http://example.invalid/fail",
			Status:   500,
			Duration: 8 * time.Millisecond,
			Failures: []string{"状态码：期望 200，实际 500"},
			Failed:   true,
		},
	}
	text, err := JSON("示例集合", results)
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if !strings.HasSuffix(text, "\n") {
		t.Fatal("报告应以换行结尾")
	}
	var report struct {
		Collection string `json:"collection"`
		Total      int    `json:"total"`
		Passed     int    `json:"passed"`
		Failed     int    `json:"failed"`
		Results    []struct {
			Name       string            `json:"name"`
			DurationMS int64             `json:"durationMs"`
			Error      string            `json:"error"`
			Failures   []string          `json:"failures"`
			Extracted  map[string]string `json:"extracted"`
			Failed     bool              `json:"failed"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(text), &report); err != nil {
		t.Fatalf("解析失败: %v\n%s", err, text)
	}
	if report.Collection != "示例集合" || report.Total != 2 || report.Passed != 1 || report.Failed != 1 {
		t.Fatalf("汇总不对：%+v", report)
	}
	if report.Results[0].Name != `登录 "用户"` {
		t.Fatalf("名称转义丢了：%q", report.Results[0].Name)
	}
	if report.Results[0].DurationMS != 1234 {
		t.Fatalf("耗时应为毫秒：%d", report.Results[0].DurationMS)
	}
	if report.Results[0].Extracted["token"] != "t-1" {
		t.Fatalf("提取变量丢了：%+v", report.Results[0].Extracted)
	}
	if len(report.Results[1].Failures) != 1 || !report.Results[1].Failed {
		t.Fatalf("失败条目不对：%+v", report.Results[1])
	}
	if strings.Contains(text, `"error": ""`) {
		t.Fatalf("空错误不该出现：\n%s", text)
	}
}
