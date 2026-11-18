//go:build ignore

// iconsgen 把 assets/icons/*.svg 编译成 internal/view/icons.go。
//
// 这是构建期工具，不链接进应用：图标在运行期就是 gfx.Icon（一串路径命令），
// 没有图片解码、没有资源目录、没有运行期 SVG 解析。
//
//	cd api-doc-go-viva && GOWORK=off go run tools/iconsgen/main.go
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/tools/iconcompile"
)

const (
	sourceDir   = "assets/icons"
	outputPath  = "internal/view/icons.go"
	packageName = "view"
	license     = "Icons compiled from assets/icons/*.svg (Lucide ISC)."
)

func main() {
	files, err := filepath.Glob(filepath.Join(sourceDir, "*.svg"))
	if err != nil || len(files) == 0 {
		fail(fmt.Errorf("没有找到 %s/*.svg: %v", sourceDir, err))
	}
	sort.Strings(files)
	icons := make(map[string]gfx.Icon, len(files))
	for _, file := range files {
		source, readErr := os.ReadFile(file)
		if readErr != nil {
			fail(readErr)
		}
		name := strings.TrimSuffix(filepath.Base(file), ".svg")
		icon, compileErr := iconcompile.Compile(name, source)
		if compileErr != nil {
			fail(compileErr)
		}
		icons[name] = icon
	}
	generated, err := iconcompile.Generate(packageName, icons, license)
	if err != nil {
		fail(err)
	}
	if err := os.WriteFile(outputPath, generated, 0o644); err != nil {
		fail(err)
	}
	fmt.Printf("iconsgen: wrote %d icons to %s\n", len(icons), outputPath)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "iconsgen:", err)
	os.Exit(1)
}
