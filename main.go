// api-doc-go-viva 是 api-doc-go 桌面客户端的 Viva 复刻验证版：
// 同一份集合文件（opencollection YAML）、同一个 Go 内核，界面换成 Viva 的原生 GPU 绘制。
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/leihenshang/api-doc-go-viva/internal/settings"
	"github.com/leihenshang/api-doc-go-viva/internal/view"
)

func main() {
	dir := flag.String("collection", "", "集合目录（必须含 opencollection.yml）；多个目录用逗号分隔")
	env := flag.String("env", "", "环境名（environments/<name>.yml，留空表示不替换变量）")
	flag.Parse()

	target, err := resolveDir(firstDir(*dir))
	if err != nil {
		log.Fatal(err)
	}
	col, err := explorer.Load(target)
	if errors.Is(err, explorer.ErrNeedsInit) {
		fmt.Fprintf(os.Stderr, "%v\n\n先用桌面客户端初始化该目录（会写入 opencollection.yml），或换一个已有集合。\n", err)
		os.Exit(2)
	}
	if err != nil {
		log.Fatal(err)
	}
	vars, err := explorer.LoadEnv(col.Dir, *env)
	if err != nil {
		log.Fatal(err)
	}
	for _, warning := range col.Warnings {
		log.Printf("跳过：%s", warning)
	}
	log.Printf("已载入「%s」：%d 条内容，环境 %q", col.Name, len(col.Entries), *env)
	model := view.New(col, *env, vars)
	for _, extra := range otherDirs(*dir) {
		other, loadErr := explorer.Load(extra)
		if loadErr != nil {
			log.Printf("跳过集合 %s：%v", extra, loadErr)
			continue
		}
		model.AddRoot(other)
		log.Printf("已载入「%s」：%d 条内容", other.Name, len(other.Entries))
	}
	if err := model.Run(); err != nil {
		log.Fatal(err)
	}
}

// firstDir 取 -collection 里的第一个目录（逗号分隔，路径本身不含逗号）。
func firstDir(value string) string {
	if index := strings.IndexByte(value, ','); index >= 0 {
		return value[:index]
	}
	return value
}

// otherDirs 取 -collection 里除第一个之外的目录。
func otherDirs(value string) []string {
	index := strings.IndexByte(value, ',')
	if index < 0 {
		return nil
	}
	var out []string
	for _, part := range strings.Split(value[index+1:], ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// resolveDir 解析集合目录：显式 -collection > 上次打开的集合 > 相邻仓库的示例集合。
func resolveDir(dir string) (string, error) {
	if dir != "" {
		return dir, nil
	}
	candidates := make([]string, 0, 3)
	if last := settings.Load().LastCollection; last != "" {
		candidates = append(candidates, last)
	}
	candidates = append(candidates,
		"../api-doc-go-flutter/sample-collection",
		"../api-doc-go-client/.tmp/sample-collection",
	)
	for _, candidate := range candidates {
		if info, err := os.Stat(filepath.Join(candidate, "opencollection.yml")); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("请用 -collection 指定集合目录，或在界面里点「打开集合」")
}
