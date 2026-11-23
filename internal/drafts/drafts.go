// Package drafts 把「还没保存的编辑」落到配置目录，重开程序还能捡回来。
//
// 为什么不写进集合目录：草稿是这台机器上的临时状态，写进集合会被 git 收进去、
// 也会让同一个集合在别人机器上出现「未保存的改动」。所以放在
// <配置目录>/drafts 下，文件名是「集合根 + 相对路径」的散列，内容是原始字节
// （请求用集合自己的 YAML 格式，文档用 UTF-8 正文）。
package drafts

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

// Kind 指草稿属于哪类内容：请求的 YAML 与文档的正文分开存。
type Kind string

const (
	Request Kind = "req"
	Doc     Kind = "doc"
)

// key 由集合根与相对路径算出来：同一个文件在同一个集合里永远落到同一个草稿。
func key(root, rel string) string {
	sum := sha256.Sum256([]byte(root + "\x00" + rel))
	return hex.EncodeToString(sum[:16])
}

// path 是草稿文件的位置（dir 由调用方给，一般是配置目录下的 drafts）。
func path(dir string, kind Kind, root, rel string) string {
	return filepath.Join(dir, string(kind)+"-"+key(root, rel)+".draft")
}

// Save 写入一份草稿（原子替换：中途崩了不会留下半个文件）。
func Save(dir string, kind Kind, root, rel string, data []byte) error {
	if dir == "" || root == "" || rel == "" || len(data) == 0 {
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	target := path(dir, kind, root, rel)
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, target)
}

// Load 读一份草稿；没有（或读不动）就当不存在。
func Load(dir string, kind Kind, root, rel string) ([]byte, bool) {
	if dir == "" || root == "" || rel == "" {
		return nil, false
	}
	data, err := os.ReadFile(path(dir, kind, root, rel))
	if err != nil || len(data) == 0 {
		return nil, false
	}
	return data, true
}

// Delete 在保存成功后清掉草稿；没有这个文件也算成功。
func Delete(dir string, kind Kind, root, rel string) {
	if dir == "" || root == "" || rel == "" {
		return
	}
	_ = os.Remove(path(dir, kind, root, rel))
}
