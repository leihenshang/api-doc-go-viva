package explorer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leihenshang/api-doc-go-share/collection"
	secretsx "github.com/leihenshang/api-doc-go-viva/internal/secrets"
)

func TestListEnvsSkipsSecretsFiles(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\n")
	write(t, filepath.Join(dir, "environments", "dev.yml"), "info:\n  name: dev\nvars: []\n")
	write(t, filepath.Join(dir, "environments", "dev.secrets.yml"), "secrets: {}\n")
	write(t, filepath.Join(dir, "environments", "prod.yml"), "info:\n  name: prod\nvars: []\n")

	names, err := ListEnvs(dir)
	if err != nil {
		t.Fatalf("ListEnvs: %v", err)
	}
	if len(names) != 2 || names[0] != "dev" || names[1] != "prod" {
		t.Fatalf("环境列表 = %v", names)
	}
}

func TestSaveEnvVarsKeepsSecretsAndPermissions(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\n")
	write(t, filepath.Join(dir, "environments", "dev.yml"), `info:
  name: dev
uid: u1
vars:
  - name: host
    value: http://old
    enabled: true
    secret: false
  - name: password
    value: ""
    enabled: true
    secret: true
`)
	write(t, filepath.Join(dir, "environments", "dev.secrets.yml"), "secrets:\n  password: s3cret\n")

	err := SaveEnvVars(dir, "dev", []collection.EnvVar{
		{Name: "host", Value: "http://new", Enabled: true},
		{Name: "user", Value: "admin", Enabled: false},
	})
	if err != nil {
		t.Fatalf("SaveEnvVars: %v", err)
	}
	vars, err := LoadEnv(dir, "dev")
	if err != nil {
		t.Fatalf("LoadEnv: %v", err)
	}
	if vars["host"] != "http://new" {
		t.Fatalf("普通变量没更新：%v", vars)
	}
	if _, ok := vars["user"]; ok {
		t.Fatalf("未启用的变量不应生效：%v", vars)
	}
	if vars["password"] != "s3cret" {
		t.Fatalf("密钥变量应原样保留（值仍在 secrets 文件）：%v", vars)
	}
	data, err := os.ReadFile(filepath.Join(dir, "environments", "dev.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "secret: true") || !strings.Contains(string(data), "password") {
		t.Fatalf("主文件应保留密钥变量行：%s", data)
	}
	info, err := os.Stat(filepath.Join(dir, "environments", "dev.yml"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("环境主文件权限应为 0600：%v %v", info, err)
	}
}

func TestCreateEnvRefusesDuplicates(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\n")
	if err := CreateEnv(dir, "staging"); err != nil {
		t.Fatalf("CreateEnv: %v", err)
	}
	if err := CreateEnv(dir, "staging"); err == nil {
		t.Fatal("同名环境应被拒绝")
	}
	if err := CreateEnv(dir, "../逃逸"); err == nil {
		t.Fatal("非法环境名应被拒绝")
	}
}

func TestSaveRequestPreservesUnknownFields(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\n")
	write(t, filepath.Join(dir, "a.yml"), `info:
  name: 旧
  type: http
  seq: 1
meta:
  uid: u1
http:
  method: GET
  url: http://old
  params: []
  headers: []
  body:
    type: none
x-custom: keep-me
`)
	col, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	entry := col.Entries[0]
	entry.Payload.HTTP.URL = "http://new"
	entry.Payload.HTTP.Method = "POST"
	if err := SaveRequest(dir, entry.File, entry.Payload); err != nil {
		t.Fatalf("SaveRequest: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "a.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "x-custom: keep-me") || !strings.Contains(text, "url: http://new") || !strings.Contains(text, "method: POST") {
		t.Fatalf("写回内容 = %s", text)
	}
}

// TestSecretsAreEncryptedAtRest 敏感值落盘是密文；读回来是明文；换密钥读不出。
func TestSecretsAreEncryptedAtRest(t *testing.T) {
	keyDir := t.TempDir()
	secretsx.Configure(keyDir, true)
	t.Cleanup(func() { secretsx.Configure("", false) })

	dir := t.TempDir()
	write(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\n")
	write(t, filepath.Join(dir, "environments", "dev.yml"), "info:\n  name: dev\nvars: []\n")

	vars := []collection.EnvVar{
		{Name: "host", Value: "https://api.example", Enabled: true},
		{Name: "token", Value: "sk-live-abcdef", Enabled: true, Secret: true},
	}
	if err := SaveEnvAll(dir, "dev", vars); err != nil {
		t.Fatalf("SaveEnvAll: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "environments", "dev.secrets.yml"))
	if err != nil {
		t.Fatalf("读密钥文件: %v", err)
	}
	if strings.Contains(string(raw), "sk-live-abcdef") {
		t.Fatalf("密钥文件里还是明文：%s", raw)
	}
	if !strings.Contains(string(raw), secretsx.Prefix) {
		t.Fatalf("没有密文前缀：%s", raw)
	}

	loaded, err := LoadEnvVars(dir, "dev")
	if err != nil {
		t.Fatalf("LoadEnvVars: %v", err)
	}
	for _, v := range loaded {
		if v.Name == "token" && v.Value != "sk-live-abcdef" {
			t.Fatalf("读回来的敏感值不对：%q", v.Value)
		}
	}

	// 发送用的变量袋同样要拿到明文。
	bag, err := LoadEnv(dir, "dev")
	if err != nil {
		t.Fatalf("LoadEnv: %v", err)
	}
	if bag["token"] != "sk-live-abcdef" || bag["host"] != "https://api.example" {
		t.Fatalf("变量袋不对：%+v", bag)
	}

	// 再存一次不能把密文当明文再套一层。
	if err := SaveEnvAll(dir, "dev", loaded); err != nil {
		t.Fatalf("二次保存: %v", err)
	}
	again, err := LoadEnvVars(dir, "dev")
	if err != nil {
		t.Fatalf("二次读取: %v", err)
	}
	for _, v := range again {
		if v.Name == "token" && v.Value != "sk-live-abcdef" {
			t.Fatalf("二次保存后值坏了：%q", v.Value)
		}
	}
}
