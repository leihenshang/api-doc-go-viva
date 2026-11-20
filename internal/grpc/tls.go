package grpc

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// tlsConfig 按 TLS 设置组装 *tls.Config：CA 可选、mTLS 需要证书与私钥成对。
func tlsConfig(cfg TLS) (*tls.Config, error) {
	config := &tls.Config{InsecureSkipVerify: cfg.InsecureSkipVerify} //nolint:gosec // 由请求文件显式开启
	if cfg.CA != "" {
		pool, err := certPool(cfg.CA)
		if err != nil {
			return nil, err
		}
		config.RootCAs = pool
	}
	if cfg.Cert != "" || cfg.Key != "" {
		if cfg.Cert == "" || cfg.Key == "" {
			return nil, fmt.Errorf("mTLS 需要同时提供证书与私钥")
		}
		pair, err := tls.LoadX509KeyPair(cfg.Cert, cfg.Key)
		if err != nil {
			return nil, fmt.Errorf("读取客户端证书: %w", err)
		}
		config.Certificates = []tls.Certificate{pair}
	}
	return config, nil
}

// certPool 读 PEM 里的 CA。
func certPool(path string) (*x509.CertPool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return nil, fmt.Errorf("CA 文件里没有可用的证书")
	}
	return pool, nil
}

// sortStrings 是 sort.Strings 的薄包装，避免每个文件都 import sort。
func sortStrings(values []string) { sort.Strings(values) }

// jsonMarshal 只在这里用一次（JSON 字符串字面量）。
func jsonMarshal(value any) ([]byte, error) { return json.Marshal(value) }
