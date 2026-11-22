package secrets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// reset 把包状态清干净：Configure 是全局的，用例之间必须互相隔离。
func reset(t *testing.T) {
	t.Helper()
	Configure("", false)
	t.Cleanup(func() { Configure("", false) })
}

// TestSealRoundTrip 加密 → 还原；密文里不能出现明文。
func TestSealRoundTrip(t *testing.T) {
	reset(t)
	dir := t.TempDir()
	Configure(dir, true)
	if !Enabled() {
		t.Fatal("Configure 后应处于加密状态")
	}

	sealed, ok := Seal("sk-live-1234567890")
	if !ok {
		t.Fatal("应加密成功")
	}
	if !strings.HasPrefix(sealed, Prefix) {
		t.Fatalf("缺少密文前缀：%q", sealed)
	}
	if strings.Contains(sealed, "sk-live") {
		t.Fatalf("密文里还看得到明文：%q", sealed)
	}
	plain, ok := Open(sealed)
	if !ok || plain != "sk-live-1234567890" {
		t.Fatalf("还原失败：%q ok=%v", plain, ok)
	}

	// 密钥文件存在且只有本人可读写。
	info, err := os.Stat(filepath.Join(dir, keyName))
	if err != nil {
		t.Fatalf("密钥文件没生成：%v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("密钥文件权限应为 0600，实际 %o", mode)
	}
}

// TestSealIsRandom 同一个明文两次加密结果不同（nonce 随机），但都能解开。
func TestSealIsRandom(t *testing.T) {
	reset(t)
	Configure(t.TempDir(), true)
	first, _ := Seal("token")
	second, _ := Seal("token")
	if first == second {
		t.Fatal("两次加密结果相同，说明 nonce 没随机")
	}
	for _, value := range []string{first, second} {
		if plain, ok := Open(value); !ok || plain != "token" {
			t.Fatalf("还原失败：%q", value)
		}
	}
}

// TestPlaintextStaysReadable 老集合里的明文不受影响，关闭加密时写入也是明文。
func TestPlaintextStaysReadable(t *testing.T) {
	reset(t)
	dir := t.TempDir()
	Configure(dir, true)
	if plain, ok := Open("just-a-plain-token"); !ok || plain != "just-a-plain-token" {
		t.Fatalf("明文应原样返回：%q ok=%v", plain, ok)
	}
	Configure(dir, false)
	sealed, ok := Seal("token")
	if ok || sealed != "token" {
		t.Fatalf("关掉加密后不该加密：%q ok=%v", sealed, ok)
	}
}

// TestWrongKeyFailsLoudly 换一个密钥目录就解不开：要返回 false，而不是给空串硬覆盖。
func TestWrongKeyFailsLoudly(t *testing.T) {
	reset(t)
	Configure(t.TempDir(), true)
	sealed, _ := Seal("secret-value")

	Configure(t.TempDir(), true) // 另一把密钥
	if plain, ok := Open(sealed); ok {
		t.Fatalf("换密钥后不该解开，实际 %q", plain)
	}
	// 批量解密要保留原样（不然写回会把密文变成空）。
	out := OpenMap(map[string]string{"token": sealed})
	if out["token"] != sealed {
		t.Fatalf("解不开时应保留原样：%q", out["token"])
	}
}

// TestSealMapRoundTrip 批量加密 / 解密。
func TestSealMapRoundTrip(t *testing.T) {
	reset(t)
	Configure(t.TempDir(), true)
	input := map[string]string{"a": "1", "b": "two"}
	sealed := SealMap(input)
	for name, value := range sealed {
		if !Sealed(value) {
			t.Fatalf("%s 没加密：%q", name, value)
		}
	}
	back := OpenMap(sealed)
	if back["a"] != "1" || back["b"] != "two" {
		t.Fatalf("批量还原不对：%+v", back)
	}
}

// TestUnconfiguredIsPassthrough 没 Configure（比如测试或工具进程）时不能崩。
func TestUnconfiguredIsPassthrough(t *testing.T) {
	reset(t)
	if Enabled() {
		t.Fatal("默认不该加密")
	}
	if sealed, ok := Seal("x"); ok || sealed != "x" {
		t.Fatalf("未配置时应直通：%q %v", sealed, ok)
	}
	if plain, ok := Open("x"); !ok || plain != "x" {
		t.Fatalf("未配置时应直通：%q %v", plain, ok)
	}
}
