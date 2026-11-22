package updater

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// withServer 起一个假的 GitHub API，并按需设好 Repo / APIBase。
func withServer(t *testing.T, status int, body string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/releases/latest") {
			t.Errorf("路径不对：%s", r.URL.Path)
		}
		if ua := r.Header.Get("User-Agent"); !strings.HasPrefix(ua, "api-doc-go-viva/") {
			t.Errorf("缺少 UA：%q", ua)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	oldBase := APIBase
	APIBase = server.URL
	t.Cleanup(func() { APIBase = oldBase })
}

// TestCheckFindsNewer 远端更新时返回版本与下载页。
func TestCheckFindsNewer(t *testing.T) {
	withServer(t, http.StatusOK, `{"tag_name":"v0.3.0","html_url":"https://example.com/r/0.3.0","body":"修了些问题"}`)
	release, err := Check(context.Background(), nil, "demo/app", "0.1.0")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if release.Version != "v0.3.0" || release.URL == "" || release.Current != "0.1.0" {
		t.Fatalf("结果不对：%+v", release)
	}
	if release.Notes != "修了些问题" {
		t.Fatalf("发布说明没读到：%q", release.Notes)
	}
}

// TestCheckUpToDate 版本相同 / 本地更新时返回 ErrUpToDate，但仍带上版本信息。
func TestCheckUpToDate(t *testing.T) {
	withServer(t, http.StatusOK, `{"tag_name":"v0.1.0","html_url":"https://example.com/r/0.1.0"}`)
	release, err := Check(context.Background(), nil, "demo/app", "0.1.0")
	if !errors.Is(err, ErrUpToDate) {
		t.Fatalf("应是最新版本，实际 err=%v", err)
	}
	if release.Version != "v0.1.0" {
		t.Fatalf("仍应带上远端版本：%+v", release)
	}

	withServer(t, http.StatusOK, `{"tag_name":"v0.1.0"}`)
	if _, err := Check(context.Background(), nil, "demo/app", "0.2.0"); !errors.Is(err, ErrUpToDate) {
		t.Fatalf("本地更新时也该算最新，实际 %v", err)
	}
}

// TestCheckErrors 没有发布、HTTP 失败、JSON 损坏都要给人话。
func TestCheckErrors(t *testing.T) {
	withServer(t, http.StatusNotFound, `{"message":"Not Found"}`)
	if _, err := Check(context.Background(), nil, "demo/app", "0.1.0"); err == nil || !strings.Contains(err.Error(), "还没有发布") {
		t.Fatalf("404 的提示不对：%v", err)
	}

	withServer(t, http.StatusInternalServerError, `{}`)
	if _, err := Check(context.Background(), nil, "demo/app", "0.1.0"); err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("500 的提示不对：%v", err)
	}

	withServer(t, http.StatusOK, `not json`)
	if _, err := Check(context.Background(), nil, "demo/app", "0.1.0"); err == nil {
		t.Fatal("坏 JSON 应报错")
	}

	withServer(t, http.StatusOK, `{"draft":true,"tag_name":"v9.9.9"}`)
	if _, err := Check(context.Background(), nil, "demo/app", "0.1.0"); err == nil {
		t.Fatal("草稿版本不该当成发布")
	}
}

// TestNewerComparesNumbers 版本比较按数字段，不按字符串（0.10 > 0.9）。
func TestNewerComparesNumbers(t *testing.T) {
	cases := []struct {
		candidate, current string
		want               bool
	}{
		{"v0.10.0", "0.9.0", true},
		{"v0.9.0", "0.10.0", false},
		{"1.0.0", "v1.0.0", false},
		{"v1.0.1-beta.1", "1.0.0", true},
		{"2", "1.9.9", true},
	}
	for _, item := range cases {
		if got := newer(item.candidate, item.current); got != item.want {
			t.Fatalf("newer(%q,%q)=%v，期望 %v", item.candidate, item.current, got, item.want)
		}
	}
}
