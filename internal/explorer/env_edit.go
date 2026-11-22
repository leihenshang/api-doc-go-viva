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
	secretsx "github.com/leihenshang/api-doc-go-viva/internal/secrets"
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

// LoadEnvVars 读环境变量（含密钥值），供环境变量弹窗编辑。
func LoadEnvVars(collectionDir, name string) ([]collection.EnvVar, error) {
	env, err := LoadEnvFile(collectionDir, name)
	if err != nil {
		return nil, err
	}
	secrets := map[string]string{}
	_, secretPath := envPaths(collectionDir, name)
	if data, readErr := os.ReadFile(secretPath); readErr == nil {
		if file, decodeErr := collection.DecodeSecretsFile(data); decodeErr == nil {
			// 磁盘上可能是密文（默认），解不开的保留原样，避免把值抹成空。
			secrets = secretsx.OpenMap(file.Secrets)
		}
	} else if !errors.Is(readErr, fs.ErrNotExist) {
		return nil, fmt.Errorf("读取环境密钥 %s: %w", name, readErr)
	}
	vars := make([]collection.EnvVar, 0, len(env.Vars))
	for _, v := range env.Vars {
		if v.Secret {
			v.Value = secrets[v.Name]
		}
		vars = append(vars, v)
	}
	return vars, nil
}

// SaveEnvAll 保存环境变量：普通值写主文件、敏感值写 *.secrets.yml；
// 变量顺序、uid、未知字段尽量保留，被删掉的敏感变量同时从密钥文件移除。
func SaveEnvAll(collectionDir, name string, vars []collection.EnvVar) error {
	if !collection.ValidEnvName(name) {
		return fmt.Errorf("环境名 %q 不合法", name)
	}
	old, err := LoadEnvFile(collectionDir, name)
	if err != nil {
		return err
	}
	_, secretPath := envPaths(collectionDir, name)
	secretsFile := &collection.SecretsFile{Secrets: map[string]string{}}
	if data, readErr := os.ReadFile(secretPath); readErr == nil {
		if file, decodeErr := collection.DecodeSecretsFile(data); decodeErr == nil {
			// 先在明文域里改，写完再整体加密：否则会把「已加密的值」再套一层。
			secretsFile = file
			secretsFile.Secrets = secretsx.OpenMap(file.Secrets)
		}
	}
	uidOf := make(map[string]string, len(old.Vars))
	wasSecret := make(map[string]bool, len(old.Vars))
	for _, v := range old.Vars {
		uidOf[v.Name] = v.UID
		if v.Secret {
			wasSecret[v.Name] = true
		}
	}
	env := &collection.EnvFile{Info: old.Info, UID: old.UID, Extra: old.Extra}
	if env.Info.Name == "" {
		env.Info.Name = name
	}
	secrets := make(map[string]string, len(secretsFile.Secrets))
	for key, value := range secretsFile.Secrets {
		secrets[key] = value
	}
	for _, v := range vars {
		fieldName := strings.TrimSpace(v.Name)
		if fieldName == "" {
			continue
		}
		if v.Secret {
			env.Vars = append(env.Vars, collection.EnvVar{UID: uidOf[fieldName], Name: fieldName, Enabled: v.Enabled, Secret: true})
			secrets[fieldName] = v.Value
			continue
		}
		env.Vars = append(env.Vars, collection.EnvVar{UID: uidOf[fieldName], Name: fieldName, Value: v.Value, Enabled: v.Enabled})
		delete(secrets, fieldName)
	}
	// 编辑器里已经没有的旧敏感变量：从密钥文件一并删掉；从未对应变量的键保留。
	for key := range secrets {
		if wasSecret[key] {
			if _, kept := envVarByName(env.Vars, key); !kept {
				delete(secrets, key)
			}
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
	if err := os.Chmod(main, 0o600); err != nil {
		return err
	}
	if len(secrets) == 0 {
		if _, statErr := os.Stat(secretPath); errors.Is(statErr, fs.ErrNotExist) {
			return nil
		}
	}
	secretData, err := (&collection.SecretsFile{
		Secrets: secretsx.SealMap(secrets), Extra: secretsFile.Extra,
	}).Encode()
	if err != nil {
		return fmt.Errorf("编码环境密钥 %s: %w", name, err)
	}
	if err := writeAtomic(secretPath, secretData); err != nil {
		return err
	}
	return os.Chmod(secretPath, 0o600)
}

// envVarByName 找同名变量；第二个返回值表示是否存在。
func envVarByName(vars []collection.EnvVar, name string) (collection.EnvVar, bool) {
	for _, v := range vars {
		if v.Name == name {
			return v, true
		}
	}
	return collection.EnvVar{}, false
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

// DeleteEnv 删除环境：主文件与密钥文件都移进 .trash，不直接抹掉。
func DeleteEnv(collectionDir, name string) error {
	if !collection.ValidEnvName(name) {
		return fmt.Errorf("环境名 %q 不合法", name)
	}
	main, secrets := envPaths(collectionDir, name)
	if _, err := os.Stat(main); errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("环境 %q 不存在", name)
	}
	if err := moveToTrash(collectionDir, main); err != nil {
		return err
	}
	if _, err := os.Stat(secrets); err == nil {
		if err := moveToTrash(collectionDir, secrets); err != nil {
			return err
		}
	}
	return nil
}

// RenameEnv 重命名环境（主文件与密钥文件一起改），目标已存在则拒绝。
func RenameEnv(collectionDir, oldName, newName string) error {
	newName = strings.TrimSpace(newName)
	if !collection.ValidEnvName(newName) {
		return fmt.Errorf("环境名只能包含中文、字母、数字与 - _ ( ) . 等字符")
	}
	if oldName == newName {
		return nil
	}
	oldMain, oldSecrets := envPaths(collectionDir, oldName)
	newMain, newSecrets := envPaths(collectionDir, newName)
	if _, err := os.Stat(oldMain); errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("环境 %q 不存在", oldName)
	}
	if _, err := os.Stat(newMain); err == nil {
		return fmt.Errorf("环境 %q 已存在", newName)
	}
	if err := os.Rename(oldMain, newMain); err != nil {
		return fmt.Errorf("重命名环境: %w", err)
	}
	if _, err := os.Stat(oldSecrets); err == nil {
		if err := os.Rename(oldSecrets, newSecrets); err != nil {
			return fmt.Errorf("重命名环境密钥文件: %w", err)
		}
	}
	return nil
}
