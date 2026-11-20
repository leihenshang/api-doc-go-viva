package send

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/leihenshang/api-doc-go-share/collection"
)

// planFor 造一条最小请求：变量与集合目录都用不到，够发一次就行。
func planFor(target string) Plan {
	return Plan{File: &collection.RequestFile{
		Info: collection.FileInfo{Name: "探测", Type: "http"},
		HTTP: collection.HTTPBlock{Method: "GET", URL: target},
	}}
}

// TestProxySendsThroughTheProxy 配了代理就走代理：请求行里是绝对地址，
// 目标域名根本不需要解析（这里用 example.invalid，DNS 一定失败）。
func TestProxySendsThroughTheProxy(t *testing.T) {
	var sawAbsolute string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAbsolute = r.URL.String()
		_, _ = fmt.Fprint(w, `{"via":"proxy"}`)
	}))
	defer proxy.Close()

	plan := planFor("http://example.invalid/hello")
	plan.Proxy = proxy.URL
	out := Do(context.Background(), plan)
	if out.Err != "" {
		t.Fatalf("应当经由代理成功：%s", out.Err)
	}
	if out.Status != 200 || !strings.Contains(out.Body, "proxy") {
		t.Fatalf("拿到的不是代理的响应：%d %q", out.Status, out.Body)
	}
	if sawAbsolute != "http://example.invalid/hello" {
		t.Fatalf("代理收到的应是绝对地址，实际 %q", sawAbsolute)
	}
}

// TestProxyAddressIsChecked 代理地址写错时给一句人话，而不是静默直连。
func TestProxyAddressIsChecked(t *testing.T) {
	plan := planFor("http://example.invalid/hello")
	plan.Proxy = "127.0.0.1:7890" // 少了 scheme
	out := Do(context.Background(), plan)
	if !strings.Contains(out.Err, "代理地址无效") {
		t.Fatalf("应提示代理地址无效，实际 %q", out.Err)
	}
}

// TestClientCertificateIsSent 双向 TLS：证书与私钥都配上时，服务端能收到客户端证书。
func TestClientCertificateIsSent(t *testing.T) {
	certPath, keyPath := writeSelfSigned(t)

	var peers int
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peers = len(r.TLS.PeerCertificates)
		_, _ = fmt.Fprint(w, `{"ok":true}`)
	}))
	srv.TLS = &tls.Config{ClientAuth: tls.RequireAnyClientCert}
	srv.StartTLS()
	defer srv.Close()

	plan := planFor(srv.URL)
	plan.InsecureSSL = true // 服务端证书是 httptest 自签的
	plan.ClientCert, plan.ClientKey = certPath, keyPath
	out := Do(context.Background(), plan)
	if out.Err != "" {
		t.Fatalf("带证书应当能连上：%s", out.Err)
	}
	if peers != 1 {
		t.Fatalf("服务端应收到 1 张客户端证书，实际 %d", peers)
	}
}

// TestBrokenClientCertificateIsReported 证书路径不对时报出来，不发无证书的请求。
func TestBrokenClientCertificateIsReported(t *testing.T) {
	dir := t.TempDir()
	plan := planFor("https://example.invalid/hello")
	plan.ClientCert, plan.ClientKey = filepath.Join(dir, "nope.crt"), filepath.Join(dir, "nope.key")
	out := Do(context.Background(), plan)
	if !strings.Contains(out.Err, "客户端证书") {
		t.Fatalf("应提示客户端证书有问题，实际 %q", out.Err)
	}
}

// writeSelfSigned 生成一对自签的客户端证书 / 私钥，写进临时目录。
func writeSelfSigned(t *testing.T) (certPath, keyPath string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("生成私钥: %v", err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "viva-client"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("签证书: %v", err)
	}
	dir := t.TempDir()
	certPath, keyPath = filepath.Join(dir, "client.crt"), filepath.Join(dir, "client.key")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatalf("写证书: %v", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatalf("写私钥: %v", err)
	}
	return certPath, keyPath
}
