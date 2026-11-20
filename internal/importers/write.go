package importers

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/leihenshang/api-doc-go-share/collection"
	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
)

// Write 把条目树落进集合：文件夹建目录，请求建文件后整块换成本次导入的内容。
// parent 是落点目录（相对集合根，空 = 根）。返回的 Summary 累计每一层的条数。
func Write(collectionDir, parent string, items []Item) (Summary, error) {
	summary := Summary{}
	writeItems(collectionDir, parent, items, &summary)
	return summary, nil
}

func writeItems(collectionDir, parent string, items []Item, summary *Summary) {
	for _, item := range items {
		if item.Request == nil {
			dir, err := explorer.CreateFolder(collectionDir, parent, item.Name)
			if err != nil {
				summary.Notes = append(summary.Notes, "跳过分组「"+item.Name+"」："+err.Error())
				continue
			}
			summary.Folders++
			writeItems(collectionDir, dir, item.Items, summary)
			continue
		}
		if err := writeRequest(collectionDir, parent, item.Request); err != nil {
			summary.Notes = append(summary.Notes, "跳过请求「"+item.Request.Info.Name+"」："+err.Error())
			continue
		}
		summary.Requests++
	}
}

// writeRequest 先让 explorer 建一个规范的新文件（拿到新 uid / seq / 去重后的文件名），
// 再把内容整块换掉——这样导入的东西和手敲的请求在磁盘上没有任何区别。
func writeRequest(collectionDir, parent string, file *collection.RequestFile) error {
	path, err := explorer.CreateRequest(collectionDir, parent, file.Info.Name, file.HTTP.Method, file.HTTP.URL)
	if err != nil {
		return err
	}
	saved, err := readRequest(collectionDir, path)
	if err != nil {
		return err
	}
	saved.HTTP = file.HTTP
	saved.Docs = file.Docs
	saved.Settings = file.Settings
	if saved.HTTP.Method == "" {
		saved.HTTP.Method = "GET"
	}
	return explorer.SaveRequest(collectionDir, path, saved)
}

// readRequest 读回刚建的文件（解出 info / meta，保留它们）。
func readRequest(collectionDir, rel string) (*collection.RequestFile, error) {
	data, err := os.ReadFile(filepath.Join(collectionDir, filepath.FromSlash(rel)))
	if err != nil {
		return nil, fmt.Errorf("读不回 %s: %w", rel, err)
	}
	file, err := collection.DecodeRequest(data)
	if err != nil {
		return nil, fmt.Errorf("解析 %s: %w", rel, err)
	}
	return file, nil
}
