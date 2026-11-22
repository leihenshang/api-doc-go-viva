// Package secrets 给「敏感值」加密：环境变量里的密钥、令牌落盘时不再是明文。
//
// 事实上的威胁模型很窄但很实在：集合目录常常整个进 git，*.secrets.yml 也在里面。
// 本包把值换成 `seal:v1:<base64(nonce|密文)>`，密钥放在用户配置目录（0600），
// 与集合分开——git 里只剩密文，换台机器没有密钥也解不开。
//
// 不做的事：不碰系统钥匙串（Linux 上要 DBus Secret Service，依赖桌面会话）；
// 不加密整份文件（那样 git diff 会变成一团二进制，也失去「值级别的迁移能力」）。
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Prefix 是密文标记。没有这个前缀的值一律当明文（老集合、手写的文件都能读）。
const Prefix = "seal:v1:"

// keyName 是密钥文件名，与 settings.json 同目录。
const keyName = "secret.key"

var (
	mu      sync.RWMutex
	keyDir  string
	sealOn  bool
	cached  []byte
	loadErr error
)

// Configure 设定密钥位置与是否加密写入。dir 为空 = 关闭（所有函数退化成直通）。
//
// 由启动流程与设置项调用；不 import settings 是为了避免 explorer → secrets →
// settings → mcp → explorer 的循环。
func Configure(dir string, seal bool) {
	mu.Lock()
	defer mu.Unlock()
	keyDir = strings.TrimSpace(dir)
	sealOn = seal && keyDir != ""
	cached, loadErr = nil, nil
}

// Enabled 报告当前是否会加密写入。
func Enabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return sealOn
}

// Sealed 报告一个值是否已经是密文。
func Sealed(value string) bool { return strings.HasPrefix(value, Prefix) }

// Seal 把明文加密成带前缀的密文；没有开启加密（或密钥不可用）时原样返回。
//
// 第二个返回值说明这次到底加密了没有：写入方据此决定要不要提示「未加密保存」。
func Seal(plain string) (string, bool) {
	if plain == "" || Sealed(plain) {
		return plain, false
	}
	block, err := aesBlock()
	if err != nil {
		return plain, false
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return plain, false
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return plain, false
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return Prefix + base64.StdEncoding.EncodeToString(sealed), true
}

// Open 把密文还原成明文；不是密文时原样返回。
//
// 解不开（换了机器、密钥丢了、文件坏了）时返回空串与 false：调用方应当保留
// 原样的密文并在界面上标出来，而不是静默当成空值覆盖掉。
func Open(value string) (string, bool) {
	if !Sealed(value) {
		return value, true
	}
	block, err := aesBlock()
	if err != nil {
		return "", false
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", false
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, Prefix))
	if err != nil || len(raw) < gcm.NonceSize() {
		return "", false
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return "", false
	}
	return string(plain), true
}

// SealMap 就地加密一批值（写盘前用）。
func SealMap(values map[string]string) map[string]string {
	out := make(map[string]string, len(values))
	for name, value := range values {
		sealed, _ := Seal(value)
		out[name] = sealed
	}
	return out
}

// OpenMap 就地解密一批值（读盘后用）；解不开的保留原样，避免把密文写回成空。
func OpenMap(values map[string]string) map[string]string {
	out := make(map[string]string, len(values))
	for name, value := range values {
		if plain, ok := Open(value); ok {
			out[name] = plain
			continue
		}
		out[name] = value
	}
	return out
}

// KeyPath 返回密钥文件路径（未 Configure 时为空）。
func KeyPath() string {
	mu.RLock()
	defer mu.RUnlock()
	if keyDir == "" {
		return ""
	}
	return filepath.Join(keyDir, keyName)
}

// aesBlock 取（必要时生成）密钥。密钥第一次用时创建，权限 0600。
func aesBlock() (cipher.Block, error) {
	mu.Lock()
	defer mu.Unlock()
	if !sealOn || keyDir == "" {
		return nil, errors.New("没有启用敏感值加密")
	}
	if loadErr != nil {
		return nil, loadErr
	}
	if cached == nil {
		key, err := readOrCreateKey(keyDir)
		if err != nil {
			loadErr = err
			return nil, err
		}
		cached = key
	}
	return aes.NewCipher(cached)
}

// readOrCreateKey 读密钥；不存在就生成一份 32 字节随机密钥。
func readOrCreateKey(dir string) ([]byte, error) {
	path := filepath.Join(dir, keyName)
	if data, err := os.ReadFile(path); err == nil {
		if len(data) != 32 {
			return nil, fmt.Errorf("%s 长度不对（应为 32 字节）", path)
		}
		return data, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("创建配置目录: %w", err)
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		return nil, fmt.Errorf("写密钥 %s: %w", path, err)
	}
	return key, nil
}
