package view

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/leihenshang/api-doc-go-viva/internal/export"
	"github.com/vivaui/viva/app"
)

// exportRow 是导出范围列表里的一行：分组行勾选会带上它下面的条目。
type exportRow struct {
	export.Row
	Selected bool
}

// openExport 打开导出弹窗：格式先定，范围默认全选。
func (m *Model) openExport(format string) {
	if m.col == nil || m.col.Dir == "" {
		return
	}
	m.ui.exportOpen = true
	m.ui.exportFormat = format
	m.ui.exportNote = ""
	rows := export.Rows([]export.Doc{{Name: m.col.Name, Col: m.col}})
	m.ui.exportRows = make([]exportRow, 0, len(rows))
	for _, row := range rows {
		m.ui.exportRows = append(m.ui.exportRows, exportRow{Row: row, Selected: true})
	}
	m.invalidator.Invalidate()
}

// toggleExportRow 勾选一行：条目行只改自己，分组行带上它下面的所有条目。
func (m *Model) toggleExportRow(index int, on bool) {
	if index < 0 || index >= len(m.ui.exportRows) {
		return
	}
	row := &m.ui.exportRows[index]
	row.Selected = on
	if !row.Group {
		return
	}
	prefix := row.Title + "/"
	for i := range m.ui.exportRows {
		target := &m.ui.exportRows[i]
		// 只带直接与间接子项：目录前缀匹配，且不带上后面同名的兄弟分组。
		if target.Group || !strings.HasPrefix(target.File, prefix) {
			continue
		}
		target.Selected = on
	}
}

// selectAllExports 全选 / 全不选。
func (m *Model) selectAllExports(on bool) {
	for index := range m.ui.exportRows {
		m.ui.exportRows[index].Selected = on
	}
	m.invalidator.Invalidate()
}

// exportSelection 收出当前勾选的条目集合。
func (m *Model) exportSelection() export.Selection {
	sel := export.Selection{}
	for _, row := range m.ui.exportRows {
		if row.Group || !row.Selected {
			continue
		}
		sel[row.File] = true
	}
	return sel
}

// exportCounts 是弹窗里「会导出多少条」的文案。
func (m *Model) exportCounts() (requests, files int) {
	return export.Count([]export.Doc{{Name: m.col.Name, Col: m.col}}, m.exportSelection())
}

// saveExport 按当前勾选生成文件：系统对话框选路径，成功后底栏回报条数与大小。
func (m *Model) saveExport() {
	sel := m.exportSelection()
	requests, files := m.exportCounts()
	if requests+files == 0 {
		m.ui.exportNote = "至少勾一条再导出"
		m.invalidator.Invalidate()
		return
	}
	content, ext, filter := m.exportContentFor(m.ui.exportFormat, sel)
	name := m.col.Name
	if strings.TrimSpace(name) == "" {
		name = "collection"
	}
	path, err := app.SaveFile(app.SaveFileOptions{
		Title:            "导出集合",
		SuggestedName:    name + "." + ext,
		DefaultExtension: ext,
		Filters:          []app.FileFilter{filter},
	})
	if err != nil {
		if !errors.Is(err, app.ErrCanceled) {
			m.ui.exportNote = "导出失败：" + err.Error()
			m.invalidator.Invalidate()
		}
		return
	}
	if path == "" {
		return
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		m.ui.exportNote = "导出失败：" + err.Error()
		m.invalidator.Invalidate()
		return
	}
	m.ui.exportOpen = false
	m.ui.exportNote = "已导出 " + filepath.Base(path) + "（" + humanBytes(len(content)) + "）"
	m.invalidator.Invalidate()
}

// exportContentFor 按格式与勾选生成正文；与「选路径 + 写文件」分开，便于无头测试。
func (m *Model) exportContentFor(format string, sel export.Selection) (string, string, app.FileFilter) {
	docs := []export.Doc{{Name: m.col.Name, Col: m.col}}
	if format == "html" {
		return export.HTMLSelected(docs, sel), "html", app.FileFilter{Name: "HTML 文档", Pattern: "*.html"}
	}
	return export.MarkdownSelected(docs, sel), "md", app.FileFilter{Name: "Markdown 文档", Pattern: "*.md"}
}
