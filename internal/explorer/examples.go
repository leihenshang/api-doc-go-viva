package explorer

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/leihenshang/api-doc-go-share/collection"
	"gopkg.in/yaml.v3"
)

// 响应示例：把一次真实响应连请求快照落成纯文本 YAML，可 git diff、可评审。
//
// 落点与 Wails 客户端一致：examples/<请求所在目录>/<请求文件名>/<示例名>.yml，
// 目录层级与集合树镜像。扫描集合时 examples 整个目录是保留目录，不当分组显示。
const examplesDirName = "examples"

// Example 一个已保存的响应示例。
type Example struct {
	Name        string
	File        string // 相对集合根
	Seq         int    // 同一请求内的保存序号，越大越新
	CreatedAt   int64
	Method      string
	URL         string
	Status      int
	DurationMS  int64
	Size        int
	ContentType string
	Body        string
	Headers     []collection.KV
}

// exampleFile 是示例的磁盘格式：info / meta / http / response 四段，与请求文件同风格。
type exampleFile struct {
	Info struct {
		Name string `yaml:"name"`
		Type string `yaml:"type"`
		Seq  int    `yaml:"seq"`
	} `yaml:"info"`
	Meta struct {
		UID        string `yaml:"uid,omitempty"`
		RequestUID string `yaml:"request_uid,omitempty"`
		CreatedAt  int64  `yaml:"created_at,omitempty"`
	} `yaml:"meta"`
	HTTP struct {
		Method  string          `yaml:"method"`
		URL     string          `yaml:"url"`
		Headers []collection.KV `yaml:"headers,omitempty"`
	} `yaml:"http"`
	Response struct {
		Status      int             `yaml:"status"`
		TimeMS      int64           `yaml:"time_ms,omitempty"`
		Size        int             `yaml:"size,omitempty"`
		ContentType string          `yaml:"content_type,omitempty"`
		Headers     []collection.KV `yaml:"headers,omitempty"`
		Body        string          `yaml:"body,omitempty"`
	} `yaml:"response"`
}

// exampleDir 是某个请求的示例目录（相对集合根）：examples/<目录>/<请求基名>。
func exampleDir(requestRel string) string {
	p := filepath.ToSlash(strings.TrimSpace(requestRel))
	return path.Join(examplesDirName, path.Dir(p), strings.TrimSuffix(path.Base(p), path.Ext(p)))
}

// Examples 列出某个请求已保存的示例，最新的在前。
// 目录不存在、单个文件坏掉都不报错：示例是附属数据，不该拖垮整个集合。
func Examples(collectionDir, requestRel string) []Example {
	full := filepath.Join(collectionDir, filepath.FromSlash(exampleDir(requestRel)))
	dir, err := os.ReadDir(full)
	if err != nil {
		return nil
	}
	out := make([]Example, 0, len(dir))
	for _, item := range dir {
		if item.IsDir() || !strings.HasSuffix(item.Name(), ".yml") {
			continue
		}
		rel := path.Join(exampleDir(requestRel), item.Name())
		data, err := os.ReadFile(filepath.Join(collectionDir, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		var file exampleFile
		if err := yaml.Unmarshal(data, &file); err != nil {
			continue
		}
		name := file.Info.Name
		if name == "" {
			name = strings.TrimSuffix(item.Name(), ".yml")
		}
		out = append(out, Example{
			Name:        name,
			File:        rel,
			Seq:         file.Info.Seq,
			CreatedAt:   file.Meta.CreatedAt,
			Method:      file.HTTP.Method,
			URL:         file.HTTP.URL,
			Status:      file.Response.Status,
			DurationMS:  file.Response.TimeMS,
			Size:        file.Response.Size,
			ContentType: file.Response.ContentType,
			Body:        file.Response.Body,
			Headers:     file.Response.Headers,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Seq != out[j].Seq {
			return out[i].Seq > out[j].Seq
		}
		return out[i].Name > out[j].Name
	})
	return out
}

// SaveExample 落一个示例；名字留空就按「时间 + 状态码」自动起名，重名自动加序号后缀。
func SaveExample(collectionDir, requestRel string, example Example) (string, error) {
	if strings.TrimSpace(requestRel) == "" {
		return "", fmt.Errorf("示例必须挂在一条请求上")
	}
	rel := exampleDir(requestRel)
	full := filepath.Join(collectionDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(full, 0o755); err != nil {
		return "", fmt.Errorf("建示例目录: %w", err)
	}
	existing := Examples(collectionDir, requestRel)
	seq := 1
	for _, item := range existing {
		if item.Seq >= seq {
			seq = item.Seq + 1
		}
	}
	if example.CreatedAt == 0 {
		example.CreatedAt = time.Now().Unix()
	}
	name := strings.TrimSpace(example.Name)
	if name == "" {
		stamp := time.Unix(example.CreatedAt, 0).Format("01-02 15:04:05")
		if example.Status > 0 {
			name = fmt.Sprintf("%s · %d", stamp, example.Status)
		} else {
			name = stamp
		}
	}
	name = uniqueExampleName(full, name)

	var file exampleFile
	file.Info.Name, file.Info.Type, file.Info.Seq = name, "response-example", seq
	file.Meta.UID, file.Meta.CreatedAt = newUID(), example.CreatedAt
	file.HTTP.Method, file.HTTP.URL = example.Method, example.URL
	file.Response.Status, file.Response.TimeMS = example.Status, example.DurationMS
	file.Response.Size, file.Response.ContentType = example.Size, example.ContentType
	file.Response.Headers, file.Response.Body = example.Headers, example.Body
	data, err := yaml.Marshal(file)
	if err != nil {
		return "", fmt.Errorf("编码示例: %w", err)
	}
	target := path.Join(rel, exampleFileName(name))
	if err := writeAtomic(filepath.Join(collectionDir, filepath.FromSlash(target)), data); err != nil {
		return "", err
	}
	return target, nil
}

// DeleteExample 删掉一个示例文件（recycle 不进回收站：示例是响应副本，随时能再存）。
func DeleteExample(collectionDir, rel string) error {
	full, err := safeJoin(collectionDir, rel)
	if err != nil {
		return err
	}
	if err := os.Remove(full); err != nil {
		return fmt.Errorf("删除示例: %w", err)
	}
	return nil
}

// exampleFileName 把示例名变成安全的文件名（去掉路径分隔符与控制字符）。
func exampleFileName(name string) string {
	cleaned := strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return '-'
		}
		if r < 0x20 {
			return -1
		}
		return r
	}, name)
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		cleaned = "示例"
	}
	// 文件名单个组件上限 255 字节；中文 3 字节，先按字节截到 180 留余量。
	for len(cleaned) > 180 {
		runes := []rune(cleaned)
		cleaned = string(runes[:len(runes)-1])
	}
	return cleaned + ".yml"
}

// uniqueExampleName 同名（含已存在的文件）就加「 (2)」「 (3)」。
func uniqueExampleName(dir, name string) string {
	candidate := name
	for n := 2; n < 1000; n++ {
		if _, err := os.Stat(filepath.Join(dir, exampleFileName(candidate))); os.IsNotExist(err) {
			return candidate
		}
		candidate = fmt.Sprintf("%s (%d)", name, n)
	}
	return fmt.Sprintf("%s (%d)", name, time.Now().Unix())
}
