package send

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	neturl "net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// jar 是进程内共享的 Cookie Jar：单条发送、集合运行器、Mock 联调都走同一份，
// 所以「登录一次，后面的请求自动带上会话」在这里是成立的。
// cookiejar 自身并发安全，只有「记录见过哪些 host」需要加锁。
var (
	jar    = newJar()
	hostMu sync.Mutex
	hosts  = map[string]bool{}

	// storeFile 非空时 jar 会落盘（Open 设置，设成空就不落盘）。
	storeMu   sync.Mutex
	storeFile string
)

// storedCookie 是落盘的一条：jar 只能按 URL 反查，所以存 URL + 名值。
type storedCookie struct {
	URL   string `json:"url"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

// storedFile 是 cookies.json 的结构。
type storedFile struct {
	Cookies []storedCookie `json:"cookies"`
}

// newJar 造一个标准的、遵循 RFC 6265 的 jar（不会失败）。
func newJar() *cookiejar.Jar {
	created, err := cookiejar.New(nil)
	if err != nil {
		// cookiejar.New(nil) 按文档不会返回错误；真出了也退回默认超时的空客户端。
		return nil
	}
	return created
}

// Jar 返回共享的 Cookie Jar（可能为 nil，调用方要么判空，要么直接用 client.Jar）。
func Jar() *cookiejar.Jar { return jar }

// noteURL 记下这次请求用过的 host：jar 只能按 URL 反查，列全部 cookie 得先知道域名。
func noteURL(target *neturl.URL) {
	if jar == nil || target == nil || target.Host == "" {
		return
	}
	key := target.Scheme + "://" + target.Host
	hostMu.Lock()
	if !hosts[key] {
		hosts[key] = true
	}
	hostMu.Unlock()
}

// Cookie 是 Cookie Jar 里的一条，给界面展示用。
//
// 只有域名 / 名称 / 值：jar.Cookies 返回的是「准备发出去的那份」，
// 已经剥掉 Path、Expires、HttpOnly 这些属性，面板也就不假装知道它们。
type Cookie struct {
	Domain string
	Name   string
	Value  string
}

// Cookies 列出 jar 里现在所有 cookie（按域名、名称排序）。
// jar 没有「遍历」接口，所以拿请求过的 host 逐个反查——这也是为什么 noteURL 必须记。
func Cookies() []Cookie {
	if jar == nil {
		return nil
	}
	hostMu.Lock()
	targets := make([]string, 0, len(hosts))
	for host := range hosts {
		targets = append(targets, host)
	}
	hostMu.Unlock()
	sort.Strings(targets)

	out := make([]Cookie, 0, len(targets))
	for _, host := range targets {
		parsed, err := neturl.Parse(host)
		if err != nil {
			continue
		}
		for _, cookie := range jar.Cookies(parsed) {
			out = append(out, Cookie{Domain: parsed.Host, Name: cookie.Name, Value: cookie.Value})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Domain != out[j].Domain {
			return out[i].Domain < out[j].Domain
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// ClearCookies 清空 jar 与「见过的 host」（面板上的「清空」）。
// 换一个新 jar 比逐个置空干净：jar 没有「删全部」接口，而旧 jar 只被这一个变量引用。
func ClearCookies() {
	hostMu.Lock()
	hosts = map[string]bool{}
	hostMu.Unlock()
	jar = newJar()
	persist()
}

// useJar 把共享 jar 挂到客户端上：没有 jar（理论上不会）就保持无状态。
func useJar(client *http.Client) *http.Client {
	if client != nil && jar != nil {
		client.Jar = jar
	}
	return client
}

// Open 让 Cookie Jar 落盘到 dir/cookies.json，并读回上次的 cookie。
// dir 为空、或读不出文件都不算错误：Cookie 是附属状态，坏了就当没有。
func Open(dir string) error {
	if dir == "" {
		Close()
		return nil
	}
	storeMu.Lock()
	storeFile = filepath.Join(dir, "cookies.json")
	storeMu.Unlock()

	data, err := os.ReadFile(filepath.Join(dir, "cookies.json"))
	if err != nil {
		return nil // 第一次运行没有这个文件
	}
	var file storedFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil // 坏文件当没有，别拖着界面起不来
	}
	for _, item := range file.Cookies {
		parsed, err := neturl.Parse(item.URL)
		if err != nil || jar == nil {
			continue
		}
		noteURL(parsed)
		jar.SetCookies(parsed, []*http.Cookie{{
			Name: item.Name, Value: item.Value, Path: "/",
		}})
	}
	return nil
}

// Close 停止落盘（设置里关掉「保存 Cookie」时）。
func Close() {
	storeMu.Lock()
	storeFile = ""
	storeMu.Unlock()
}

// persist 把 jar 里现在的 cookie 写回磁盘；没开落盘、或写失败都静默。
// 在每次真实发送之后调用：cookie 只会因为请求而变。
func persist() {
	storeMu.Lock()
	target := storeFile
	storeMu.Unlock()
	if target == "" {
		return
	}
	hostMu.Lock()
	targets := make([]string, 0, len(hosts))
	for host := range hosts {
		targets = append(targets, host)
	}
	hostMu.Unlock()

	file := storedFile{Cookies: make([]storedCookie, 0, len(targets))}
	for _, host := range targets {
		parsed, err := neturl.Parse(host)
		if err != nil {
			continue
		}
		for _, cookie := range jar.Cookies(parsed) {
			file.Cookies = append(file.Cookies, storedCookie{URL: host, Name: cookie.Name, Value: cookie.Value})
		}
	}
	sort.Slice(file.Cookies, func(i, j int) bool {
		if file.Cookies[i].URL != file.Cookies[j].URL {
			return file.Cookies[i].URL < file.Cookies[j].URL
		}
		return file.Cookies[i].Name < file.Cookies[j].Name
	})
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return
	}
	// 先写临时文件再改名：中途崩了不会留下半个 JSON。
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, target)
}

// DeleteCookie 删掉某个域名下的一条 cookie（面板里的「删除」）。
func DeleteCookie(domain, name string) error {
	if jar == nil {
		return fmt.Errorf("Cookie Jar 不可用")
	}
	parsed, err := neturl.Parse("http://" + strings.TrimSpace(domain))
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("域名无效: %q", domain)
	}
	// MaxAge < 0 等于让 jar 立刻丢弃它。
	jar.SetCookies(parsed, []*http.Cookie{{Name: name, Value: "", Path: "/", MaxAge: -1}})
	persist()
	return nil
}
