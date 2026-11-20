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
	"github.com/leihenshang/api-doc-go-viva/internal/send"
	"github.com/leihenshang/api-doc-go-viva/internal/settings"
	"github.com/leihenshang/api-doc-go-viva/internal/view"
)

func main() {
	dir := flag.String("collection", "", "集合目录（必须含 opencollection.yml）；多个目录用逗号分隔")
	env := flag.String("env", "", "环境名（environments/<name>.yml，留空表示不替换变量）")
	flag.Parse()

	explicit := strings.TrimSpace(*dir) != ""
	target, err := resolveDir(firstDir(*dir))
	var col *explorer.Collection
	var vars map[string]string
	var reopen []string
	if !explicit {
		reopen = startupCollections()
	}
	if err != nil && explicit {
		log.Fatal(err)
	}
	if !explicit && len(reopen) > 0 {
		col = loadStartup(reopen[0], false)
		if col != nil {
			reopen = reopen[1:]
		}
	} else if err == nil {
		col = loadStartup(target, explicit)
	} else {
		// 双击启动 / 上次集合不可用：进界面后点「打开集合」，不要直接退出。
		log.Printf("没有可用的集合：%v", err)
	}
	if col != nil {
		vars, err = explorer.LoadEnv(col.Dir, *env)
		if err != nil {
			if explicit {
				log.Fatal(err)
			}
			log.Printf("读取环境 %q 失败：%v", *env, err)
		}
		log.Printf("已载入「%s」：%d 条内容，环境 %q", col.Name, len(col.Entries), *env)
	}
	// Cookie 默认落盘（设置里可关）：登录一次，下次打开还在。
	if cfg := settings.Load(); cfg.PersistCookies() {
		if cfgDir, cfgErr := settings.Dir(); cfgErr == nil {
			if err := send.Open(cfgDir); err != nil {
				log.Printf("读取 Cookie 失败：%v", err)
			}
		}
	}
	model := view.New(col, *env, vars)
	for _, extra := range append(reopen, otherDirs(*dir)...) {
		if extra == "" {
			continue
		}
		other, loadErr := explorer.Load(extra)
		if loadErr != nil {
			log.Printf("跳过集合 %s：%v", extra, loadErr)
			continue
		}
		model.AddRoot(other)
		log.Printf("已载入「%s」：%d 条内容", other.Name, len(other.Entries))
	}
	if !explicit {
		model.ActivateCollection(settings.Load().ActiveCollection)
	}
	if err := model.Run(); err != nil {
		log.Fatal(err)
	}
}

// startupCollections 是上次同时打开的集合根；老配置只有 LastCollection 时退回它。
func startupCollections() []string {
	st := settings.Load()
	if len(st.LastCollections) > 0 {
		return st.LastCollections
	}
	if st.LastCollection != "" {
		return []string{st.LastCollection}
	}
	return nil
}

// loadStartup 读启动集合：显式指定的路径读不动就退出（用户要看得到错误）；
// 自动回落 / 上次集合读不动时返回 nil，让界面进空状态再由用户点「打开集合」。
func loadStartup(target string, explicit bool) *explorer.Collection {
	col, err := explorer.Load(target)
	if err == nil {
		for _, warning := range col.Warnings {
			log.Printf("跳过：%s", warning)
		}
		return col
	}
	if explicit {
		if errors.Is(err, explorer.ErrNeedsInit) {
			fmt.Fprintf(os.Stderr, "%v\n\n先用桌面客户端初始化该目录（会写入 opencollection.yml），或换一个已有集合。\n", err)
			os.Exit(2)
		}
		log.Fatal(err)
	}
	log.Printf("跳过集合 %s：%v", target, err)
	return nil
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
