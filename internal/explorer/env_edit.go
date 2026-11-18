package explorer

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/leihenshang/api-doc-go-share/collection"
)

// envDir 是环境文件所在目录（与客户端一致）。
const envDir = "environments"

// ListEnvs 列出集合里的环境名：environments/*.yml，排除 *.secrets.yml。
func ListEnvs(collectionDir string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(collectionDir, envDir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取环境目录: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".secrets.yml") {
			continue
		}
		names = append(names, strings.TrimSuffix(name, ".yml"))
	}
	sort.Strings(names)
	return names, nil
}

// envPaths 返回环境主文件与密钥文件路径。
func envPaths(collectionDir, name string) (main, secrets string) {
	base := filepath.Join(collectionDir, envDir, name)
	return base + ".yml", base + ".secrets.yml"
}

// LoadEnvFile 读环境主文件；不存在时返回一个空环境（名字已设好）。
func LoadEnvFile(collectionDir, name string) (*collection.EnvFile, error) {
	if !collection.ValidEnvName(name) {
		return nil, fmt.Errorf("环境名 %q 不合法", name)
	}
	main, _ := envPaths(collectionDir, name)
	data, err := os.ReadFile(main)
	if errors.Is(err, fs.ErrNotExist) {
		env := &collection.EnvFile{}
		env.Info.Name = name
		return env, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取环境 %s: %w", name, err)
	}
	return collection.DecodeEnv(data)
}

// SaveEnvVars 保存环境变量：只改传入的非密钥变量，密钥变量与 .secrets.yml 原样保留。
// 与客户端一致：主文件 0600（里面常有内网地址/令牌）。
func SaveEnvVars(collectionDir, name string, vars []collection.EnvVar) error {
	if !collection.ValidEnvName(name) {
		return fmt.Errorf("环境名 %q 不合法", name)
	}
	old, err := LoadEnvFile(collectionDir, name)
	if err != nil {
		return err
	}
	env := &collection.EnvFile{Info: old.Info, UID: old.UID, Extra: old.Extra}
	if env.Info.Name == "" {
		env.Info.Name = name
	}
	for _, v := range vars {
		if v.Secret || strings.TrimSpace(v.Name) == "" {
			continue
		}
		env.Vars = append(env.Vars, collection.EnvVar{Name: v.Name, Value: v.Value, Enabled: v.Enabled})
	}
	// 密钥变量保持原样（值在 .secrets.yml，主文件里是空串）。
	for _, v := range old.Vars {
		if v.Secret {
			v.Value = ""
			env.Vars = append(env.Vars, v)
		}
	}
	data, err := env.Encode()
	if err != nil {
		return fmt.Errorf("编码环境 %s: %w", name, err)
	}
	main, _ := envPaths(collectionDir, name)
	if err := os.MkdirAll(filepath.Dir(main), 0o755); err != nil {
		return fmt.Errorf("创建环境目录: %w", err)
	}
	if err := writeAtomic(main, data); err != nil {
		return err
	}
	return os.Chmod(main, 0o600)
}

// CreateEnv 新建一个空环境；同名已存在时报错（不覆盖）。
func CreateEnv(collectionDir, name string) error {
	name = strings.TrimSpace(name)
	if !collection.ValidEnvName(name) {
		return fmt.Errorf("环境名只能包含中文、字母、数字与 - _ ( ) . 等字符")
	}
	main, _ := envPaths(collectionDir, name)
	if _, err := os.Stat(main); err == nil {
		return fmt.Errorf("环境 %q 已存在", name)
	}
	env := &collection.EnvFile{UID: newUID()}
	env.Info.Name = name
	data, err := env.Encode()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(main), 0o755); err != nil {
		return fmt.Errorf("创建环境目录: %w", err)
	}
	if err := writeAtomic(main, data); err != nil {
		return err
	}
	return os.Chmod(main, 0o600)
}
