package send

import (
	"net/http"
	"testing"
)

// TestIsFileResponse 只有「明确要下载」或「不是文本」才算文件；JSON / 文本照常渲染。
func TestIsFileResponse(t *testing.T) {
	cases := []struct {
		name     string
		outcome  Outcome
		wantFile bool
	}{
		{"json", Outcome{ContentType: "application/json; charset=utf-8"}, false},
		{"纯文本", Outcome{ContentType: "text/plain"}, false},
		{"html", Outcome{ContentType: "text/html; charset=utf-8"}, false},
		{"没给类型", Outcome{}, false},
		{"图片", Outcome{ContentType: "image/png"}, true},
		{"pdf", Outcome{ContentType: "application/pdf"}, true},
		{"二进制流", Outcome{ContentType: "application/octet-stream"}, true},
		{"带 filename", Outcome{ContentType: "application/json", Filename: "a.json"}, true},
	}
	for _, tc := range cases {
		if got := tc.outcome.IsFile(); got != tc.wantFile {
			t.Errorf("%s: IsFile = %v，期望 %v", tc.name, got, tc.wantFile)
		}
	}
}

// TestFilenameFromDisposition 从 Content-Disposition 取名字，路径会被剥掉。
func TestFilenameFromDisposition(t *testing.T) {
	cases := []struct {
		header string
		want   string
	}{
		{`attachment; filename="report.pdf"`, "report.pdf"},
		{`attachment; filename=../../etc/passwd`, "passwd"},
		{`inline`, ""},
		{``, ""},
		{`attachment; filename*=UTF-8''%E6%8A%A5%E5%91%8A.pdf`, "报告.pdf"},
	}
	for _, tc := range cases {
		header := http.Header{}
		if tc.header != "" {
			header.Set("Content-Disposition", tc.header)
		}
		if got := filenameFrom(header); got != tc.want {
			t.Errorf("%q → %q，期望 %q", tc.header, got, tc.want)
		}
	}
}

// TestSuggestedFileName 没有文件名时按类型给默认名。
func TestSuggestedFileName(t *testing.T) {
	if got := (Outcome{ContentType: "image/png"}).SuggestedFileName(); got != "response.bin" {
		t.Errorf("图片默认名 = %q", got)
	}
	if got := (Outcome{ContentType: "application/json"}).SuggestedFileName(); got != "response.json" {
		t.Errorf("JSON 默认名 = %q", got)
	}
	if got := (Outcome{Filename: "导出.csv"}).SuggestedFileName(); got != "导出.csv" {
		t.Errorf("有文件名时应直接用：%q", got)
	}
}
