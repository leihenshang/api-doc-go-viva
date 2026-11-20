package explorer

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/leihenshang/api-doc-go-share/collection"
	"gopkg.in/yaml.v3"
)

// 集合级公共参数（清单 opencollection.yml 的 defaults 段）：
//   defaults:
//     headers: [...]        # 每条请求都带上；请求里同名头以请求为准
//     auth: {type: bearer}  # 认证类型为 inherit 的请求用它
//
// 与客户端同一处落点，改了在 Wails 版里也能读到。

// manifestFile 是集合清单文件名。
const manifestFile = "opencollection.yml"

// LoadDefaults 读集合的公共参数；没有清单或没有 defaults 段都返回零值。
func LoadDefaults(dir string) collection.Defaults {
	data, err := os.ReadFile(filepath.Join(dir, manifestFile))
	if err != nil {
		return collection.Defaults{}
	}
	manifest, err := collection.DecodeManifest(data)
	if err != nil || manifest.Defaults == nil {
		return collection.Defaults{}
	}
	return *manifest.Defaults
}

// SaveDefaults 把公共参数写回清单：只动 defaults，info / meta / 未知顶层字段原样保留。
// 全空时删掉这一段，不留一个空壳。
func SaveDefaults(dir string, def collection.Defaults) error {
	full := filepath.Join(dir, manifestFile)
	data, err := os.ReadFile(full)
	if err != nil {
		return fmt.Errorf("读取集合清单: %w", err)
	}
	manifest, err := collection.DecodeManifest(data)
	if err != nil {
		return fmt.Errorf("解析集合清单: %w", err)
	}
	if len(def.Headers) == 0 && def.Auth == nil {
		manifest.Defaults = nil
	} else {
		copied := def
		manifest.Defaults = &copied
	}
	out, err := yaml.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("编码集合清单: %w", err)
	}
	return writeAtomic(full, out)
}
