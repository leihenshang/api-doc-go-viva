package send

import (
	"net/http"
	"net/http/cookiejar"
	neturl "net/url"
	"sort"
	"sync"
)

// jar 是进程内共享的 Cookie Jar：单条发送、集合运行器、Mock 联调都走同一份，
// 所以「登录一次，后面的请求自动带上会话」在这里是成立的。
// cookiejar 自身并发安全，只有「记录见过哪些 host」需要加锁。
var (
	jar    = newJar()
	hostMu sync.Mutex
	hosts  = map[string]bool{}
)

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
}

// useJar 把共享 jar 挂到客户端上：没有 jar（理论上不会）就保持无状态。
func useJar(client *http.Client) *http.Client {
	if client != nil && jar != nil {
		client.Jar = jar
	}
	return client
}
