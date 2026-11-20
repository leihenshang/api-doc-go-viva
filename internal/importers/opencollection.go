package importers

import (
	"fmt"
	"sort"
	"strings"

	"github.com/leihenshang/api-doc-go-share/collection"
	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
)

// OpenCollection 把另一个 OpenCollection 集合目录里的条目搬进当前集合。
//
// 目标是「同一个格式的复制」，所以按解析结果重建：分组照层级建，请求走
// explorer.CreateRequest + SaveRequest（新 uid、新 seq、同名加 -2），文档走
// CreateDoc + SaveDoc。源集合不动。
func OpenCollection(dstDir, parent, srcDir string) (Summary, error) {
	src, err := explorer.Load(srcDir)
	if err != nil {
		return Summary{}, fmt.Errorf("读源集合: %w", err)
	}
	if src.Dir == dstDir {
		return Summary{}, fmt.Errorf("源集合和目标集合是同一个目录")
	}
	summary := Summary{}
	// 分组先建：按层级深度排序，父目录一定先于子目录出现。
	folders := append([]*explorer.Folder(nil), src.Folders...)
	sort.Slice(folders, func(i, j int) bool {
		depthI, depthJ := strings.Count(folders[i].Dir, "/"), strings.Count(folders[j].Dir, "/")
		if depthI != depthJ {
			return depthI < depthJ
		}
		return folders[i].Dir < folders[j].Dir
	})
	mapped := make(map[string]string, len(folders)) // 源相对目录 → 目标相对目录
	for _, folder := range folders {
		// docs/ 是文档的默认落点（与本版 CreateDoc 一致），不当作业务分组复制。
		if folder.Dir == "docs" {
			continue
		}
		targetParent := parent
		if slash := strings.LastIndex(folder.Dir, "/"); slash >= 0 {
			if mappedParent, ok := mapped[folder.Dir[:slash]]; ok {
				targetParent = mappedParent
			} else {
				summary.Notes = append(summary.Notes, "跳过分组「"+folder.Name+"」：父目录没建起来")
				continue
			}
		}
		dir, err := explorer.CreateFolder(dstDir, targetParent, folder.Name)
		if err != nil {
			summary.Notes = append(summary.Notes, "跳过分组「"+folder.Name+"」："+err.Error())
			continue
		}
		mapped[folder.Dir] = dir
		summary.Folders++
	}

	for _, entry := range src.Entries {
		targetFolder := folderOf(mapped, entry.File)
		if entry.IsDoc() {
			name := entry.Name
			if name == "" {
				name = "导入的文档"
			}
			rel, err := explorer.CreateDoc(dstDir, name)
			if err != nil {
				summary.Notes = append(summary.Notes, "跳过文档「"+name+"」："+err.Error())
				continue
			}
			if err := explorer.SaveDoc(dstDir, rel, entry.Front, entry.Content); err != nil {
				summary.Notes = append(summary.Notes, "跳过文档「"+name+"」："+err.Error())
				continue
			}
			summary.Docs++
			if targetFolder != "" {
				summary.Notes = append(summary.Notes, "文档「"+name+"」落在 docs/，"+
					"源里的分组「"+targetFolder+"」没有一并复制")
			}
			continue
		}
		if entry.Payload == nil {
			continue
		}
		payload := *entry.Payload
		if entry.Name != "" {
			payload.Info.Name = entry.Name
		}
		if err := writeRequest(dstDir, targetFolder, &payload); err != nil {
			summary.Notes = append(summary.Notes, "跳过请求「"+entry.Name+"」："+err.Error())
			continue
		}
		summary.Requests++
	}
	if summary.Requests == 0 && summary.Docs == 0 {
		return summary, fmt.Errorf("源集合里没有可导入的条目")
	}
	return summary, nil
}

// folderOf 由条目的相对路径找出它在目标集合里的落点目录。
func folderOf(mapped map[string]string, file string) string {
	slash := strings.LastIndex(file, "/")
	if slash < 0 {
		return ""
	}
	if dir, ok := mapped[file[:slash]]; ok {
		return dir
	}
	return ""
}

// VarsOf 读源集合某个环境里的普通变量（导入时带过来的那部分）。
func VarsOf(srcDir, env string) []collection.EnvVar {
	if strings.TrimSpace(env) == "" {
		return nil
	}
	vars, err := explorer.LoadEnvVars(srcDir, env)
	if err != nil {
		return nil
	}
	return vars
}
