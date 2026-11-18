package explorer

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/leihenshang/api-doc-go-share/collection"
)

// InitCollection 在目录里写一份最简 opencollection.yml。
//
// 与客户端同一条闸门：只有用户明确确认后才调用；已有清单时拒绝（不覆盖别人的集合）。
func InitCollection(dir, name string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("解析集合路径: %w", err)
	}
	if info, statErr := os.Stat(abs); statErr != nil || !info.IsDir() {
		return fmt.Errorf("目录不可用: %s", abs)
	}
	manifestPath := filepath.Join(abs, manifestName)
	if _, statErr := os.Stat(manifestPath); statErr == nil {
		return fmt.Errorf("目录里已有 %s，不覆盖", manifestName)
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return fmt.Errorf("检查 %s: %w", manifestName, statErr)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = filepath.Base(abs)
	}
	manifest := &collection.Manifest{}
	manifest.Info.Name = name
	manifest.Meta.UID = newUID()
	data, err := manifest.Encode("1.0.0")
	if err != nil {
		return fmt.Errorf("编码集合清单: %w", err)
	}
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		return fmt.Errorf("写入集合清单: %w", err)
	}
	return nil
}
