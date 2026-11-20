package send

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leihenshang/api-doc-go-share/collection"
)

// TestCookieJarIsSharedAcrossSends 登录接口种下的 cookie，下一条请求自动带上——
// 单条发送与集合运行器用的是同一个 jar。
func TestCookieJarIsSharedAcrossSends(t *testing.T) {
	ClearCookies()
	t.Cleanup(ClearCookies)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			http.SetCookie(w, &http.Cookie{Name: "sid", Value: "abc123", Path: "/", HttpOnly: true})
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			cookie, err := r.Cookie("sid")
			if err != nil {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"sid":"` + cookie.Value + `"}`))
		}
	}))
	defer srv.Close()

	login := Plan{File: &collection.RequestFile{
		Info: collection.FileInfo{Name: "登录", Type: "http"},
		HTTP: collection.HTTPBlock{Method: "POST", URL: srv.URL + "/login"},
	}}
	if out := Do(context.Background(), login); out.Err != "" {
		t.Fatalf("登录失败：%+v", out)
	}

	// jar 里应当有这条 cookie。
	cookies := Cookies()
	if len(cookies) != 1 || cookies[0].Name != "sid" || cookies[0].Value != "abc123" {
		t.Fatalf("Cookie Jar 没记下来：%+v", cookies)
	}

	// 下一条请求自动带上。
	probe := Plan{File: &collection.RequestFile{
		Info: collection.FileInfo{Name: "会话", Type: "http"},
		HTTP: collection.HTTPBlock{Method: "GET", URL: srv.URL + "/me"},
	}}
	out := Do(context.Background(), probe)
	if out.Status != 200 || !strings.Contains(out.Body, "abc123") {
		t.Fatalf("第二条没带上 cookie：%+v", out)
	}

	// 清空后就该重新变成未登录。
	ClearCookies()
	if left := Cookies(); len(left) != 0 {
		t.Fatalf("清空后还有 cookie：%+v", left)
	}
	if out := Do(context.Background(), probe); out.Status != http.StatusUnauthorized {
		t.Fatalf("清空后不该再带上 cookie：%+v", out)
	}
}

// TestCookiesAreSorted 面板按域名、名称排序显示，多次发送不重复列出。
func TestCookiesAreSorted(t *testing.T) {
	ClearCookies()
	t.Cleanup(ClearCookies)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "b", Value: "2", Path: "/"})
		http.SetCookie(w, &http.Cookie{Name: "a", Value: "1", Path: "/"})
	}))
	defer srv.Close()

	plan := Plan{File: &collection.RequestFile{
		Info: collection.FileInfo{Name: "种 cookie", Type: "http"},
		HTTP: collection.HTTPBlock{Method: "GET", URL: srv.URL + "/set"},
	}}
	Do(context.Background(), plan)
	Do(context.Background(), plan) // 发两次不该把 host 记两遍

	cookies := Cookies()
	if len(cookies) != 2 {
		t.Fatalf("应有两条 cookie：%+v", cookies)
	}
	if cookies[0].Name != "a" || cookies[1].Name != "b" {
		t.Fatalf("没按名称排序：%+v", cookies)
	}
}

// resetJar 模拟「重启进程」：清掉内存里的 jar，但不动磁盘上的 cookies.json。
func resetJar() {
	hostMu.Lock()
	hosts = map[string]bool{}
	hostMu.Unlock()
	jar = newJar()
}

// TestCookiesPersistAcrossRestart 落盘后重启仍在；单条删除只影响那一条。
func TestCookiesPersistAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	ClearCookies()
	t.Cleanup(func() { ClearCookies(); Close() })
	if err := Open(dir); err != nil {
		t.Fatalf("Open: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "sid", Value: "s-1", Path: "/"})
		http.SetCookie(w, &http.Cookie{Name: "lang", Value: "zh", Path: "/"})
	}))
	defer srv.Close()
	plan := Plan{File: &collection.RequestFile{
		Info: collection.FileInfo{Name: "登录", Type: "http"},
		HTTP: collection.HTTPBlock{Method: "GET", URL: srv.URL + "/login"},
	}}
	Do(context.Background(), plan)

	data, err := os.ReadFile(filepath.Join(dir, "cookies.json"))
	if err != nil {
		t.Fatalf("cookies.json 没落盘：%v", err)
	}
	if !strings.Contains(string(data), `"sid"`) || !strings.Contains(string(data), `"s-1"`) {
		t.Fatalf("落盘内容不对：%s", data)
	}

	// 重启：内存清空，只从磁盘读。
	resetJar()
	if left := Cookies(); len(left) != 0 {
		t.Fatalf("重置后内存里不该还有：%+v", left)
	}
	if err := Open(dir); err != nil {
		t.Fatalf("重启后 Open: %v", err)
	}
	restored := Cookies()
	if len(restored) != 2 {
		t.Fatalf("重启后应读回两条：%+v", restored)
	}

	// 单条删除：只掉 lang。
	if err := DeleteCookie(restored[0].Domain, "lang"); err != nil {
		t.Fatalf("DeleteCookie: %v", err)
	}
	after := Cookies()
	if len(after) != 1 || after[0].Name != "sid" {
		t.Fatalf("删单条后应只剩 sid：%+v", after)
	}
	// 文件也同步了。
	data, _ = os.ReadFile(filepath.Join(dir, "cookies.json"))
	if strings.Contains(string(data), `"lang"`) {
		t.Fatalf("文件里还有被删的那条：%s", data)
	}

	// 关掉落盘后不再写文件：清空后再发一次，文件仍旧是旧内容。
	Close()
	if err := DeleteCookie(after[0].Domain, "sid"); err != nil {
		t.Fatalf("DeleteCookie: %v", err)
	}
	data, _ = os.ReadFile(filepath.Join(dir, "cookies.json"))
	if !strings.Contains(string(data), `"sid"`) {
		t.Fatalf("关掉落盘后不该再写文件：%s", data)
	}
}
