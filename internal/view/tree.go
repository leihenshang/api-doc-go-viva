package view

import (
	"path"
	"sort"
	"strings"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/vivaui/viva/ui"
)

// 集合树的三种行：集合根、分组目录、条目（请求 / gRPC / 文档）。
// 树是「可见行」的扁平切片：展开状态、过滤词一变就重建，绘制期只读。
type treeKind uint8

const (
	nodeRoot treeKind = iota
	nodeFolder
	nodeEntry
)

// treeRow 是集合树的一行。
type treeRow struct {
	kind     treeKind
	depth    int
	key      string // 展开状态的键：root:<dir> / dir:<相对路径> / entry:<文件>
	label    string // 无障碍与测试钩子
	name     string // 画在行里的名字
	root     int    // 属于第几个集合根
	entry    *explorer.Entry
	folder   *explorer.Folder
	branch   bool
	expanded bool
}

// treeChildren 是一次重建用的子节点索引：目录 → 子目录 / 条目，避免每层都扫全表。
type treeChildren struct {
	folders map[string][]*explorer.Folder
	entries map[string][]*explorer.Entry
	counts  map[string]int
}

// parentDir 取相对路径的父目录（根目录用空串）。
func parentDir(rel string) string {
	dir := path.Dir(rel)
	if dir == "." || dir == "/" {
		return ""
	}
	return dir
}

// requestDir 是请求文件所在的分组目录（根目录用空串）。
func requestDir(file string) string { return parentDir(file) }

// newTreeChildren 按父目录归类并排序：目录在前、条目在后，各自按 seq 再按名称。
func newTreeChildren(col *explorer.Collection) *treeChildren {
	kids := &treeChildren{
		folders: make(map[string][]*explorer.Folder, len(col.Folders)),
		entries: make(map[string][]*explorer.Entry, len(col.Entries)),
		counts:  make(map[string]int),
	}
	for _, folder := range col.Folders {
		parent := parentDir(folder.Dir)
		kids.folders[parent] = append(kids.folders[parent], folder)
	}
	for _, folders := range kids.folders {
		sort.SliceStable(folders, func(i, j int) bool {
			if folders[i].Seq != folders[j].Seq {
				return folders[i].Seq < folders[j].Seq
			}
			return folders[i].Name < folders[j].Name
		})
	}
	for _, entry := range col.Entries {
		dir := parentDir(entry.File)
		kids.entries[dir] = append(kids.entries[dir], entry)
	}
	for _, entries := range kids.entries {
		sort.SliceStable(entries, func(i, j int) bool {
			if entries[i].Seq != entries[j].Seq {
				return entries[i].Seq < entries[j].Seq
			}
			return entries[i].Name < entries[j].Name
		})
	}
	return kids
}

// countMatches 统计某目录（含子目录）里匹配过滤词的条目数，过滤时决定哪些枝干留下。
func (m *Model) countMatches(kids *treeChildren, dir, needle string) int {
	if count, ok := kids.counts[dir]; ok {
		return count
	}
	count := 0
	for _, entry := range kids.entries[dir] {
		if needle == "" || entryMatches(entry, needle) {
			count++
		}
	}
	for _, folder := range kids.folders[dir] {
		count += m.countMatches(kids, folder.Dir, needle)
	}
	kids.counts[dir] = count
	return count
}

// rebuildTree 重建可见行：过滤词为空时显示全部，非空时只留下有匹配的枝干并强制展开。
func (m *Model) rebuildTree() {
	m.treeDirty = false
	m.rows = m.rows[:0]
	needle := strings.ToLower(strings.TrimSpace(m.filter))
	force := needle != ""

	for index, root := range m.roots {
		col := root.col
		kids := newTreeChildren(col)
		m.countMatches(kids, "", needle)
		if force && kids.counts[""] == 0 {
			continue
		}
		rootKey := "root:" + col.Dir
		branch := len(kids.folders[""])+len(kids.entries[""]) > 0
		expanded := force || m.treeExpanded(rootKey, index == m.active)
		m.rows = append(m.rows, treeRow{
			kind:     nodeRoot,
			key:      rootKey,
			label:    "集合 " + col.Name,
			name:     col.Name,
			root:     index,
			branch:   branch,
			expanded: expanded && branch,
		})
		if expanded && branch {
			m.appendChildren(index, "", 1, needle, force, kids)
		}
	}
}

// appendChildren 追加某目录下的分组与条目行（只在展开时递归）。
func (m *Model) appendChildren(root int, dir string, depth int, needle string, force bool, kids *treeChildren) {
	for _, folder := range kids.folders[dir] {
		if force && kids.counts[folder.Dir] == 0 {
			continue
		}
		key := "dir:" + folder.Dir
		branch := len(kids.folders[folder.Dir])+len(kids.entries[folder.Dir]) > 0
		expanded := force || m.treeExpanded(key, true)
		m.rows = append(m.rows, treeRow{
			kind:     nodeFolder,
			depth:    depth,
			key:      key,
			label:    folder.Name,
			name:     folder.Name,
			root:     root,
			folder:   folder,
			branch:   branch,
			expanded: expanded && branch,
		})
		if expanded && branch {
			m.appendChildren(root, folder.Dir, depth+1, needle, force, kids)
		}
	}
	for _, entry := range kids.entries[dir] {
		if force && !entryMatches(entry, needle) {
			continue
		}
		m.rows = append(m.rows, treeRow{
			kind:  nodeEntry,
			depth: depth,
			key:   "entry:" + entry.File,
			label: entry.Title(),
			name:  entry.Name,
			root:  root,
			entry: entry,
		})
	}
}

// treeExpanded 查展开状态；没记录过就用默认值。
func (m *Model) treeExpanded(key string, fallback bool) bool {
	if expanded, ok := m.expanded[key]; ok {
		return expanded
	}
	return fallback
}

// syncTree 在状态变化后重建可见行（每帧只在脏的时候做一次）。
func (m *Model) syncTree() {
	if m.treeDirty {
		m.rebuildTree()
	}
}

// treeNode 描述某一行的层级、是否枝干与展开状态，交给 Tree 画缩进与箭头。
func (m *Model) treeNode(i int) ui.TreeNode {
	row := &m.rows[i]
	return ui.TreeNode{
		Depth:    row.depth,
		Branch:   row.branch,
		Expanded: row.expanded,
		Selected: row.key == m.selKey,
	}
}

// treeLabel 是无障碍名与测试钩子：请求是「GET 名称」，文档是「MD 名称」。
func (m *Model) treeLabel(i int) string { return m.rows[i].label }

// treeKey 给行一个稳定代号：展开/折叠导致行号变化时，焦点与选择跟着条目走。
func (m *Model) treeKey(i int) int { return treeHash(m.rows[i].key) }

// treeHash 是 32 位 FNV-1a，取正数：不给每帧分配一个 hasher。
func treeHash(key string) int {
	var hash uint32 = 2166136261
	for i := 0; i < len(key); i++ {
		hash ^= uint32(key[i])
		hash *= 16777619
	}
	return int(hash & 0x7fffffff)
}

// toggleRow 展开 / 收起一行；点击分组行与点箭头是同一个动作。
func (m *Model) toggleRow(i int) {
	if i < 0 || i >= len(m.rows) {
		return
	}
	row := &m.rows[i]
	if !row.branch {
		return
	}
	m.expanded[row.key] = !row.expanded
	m.treeDirty = true
	m.syncTree()
}

// chooseRow 处理点击 / 回车：集合根切过去并展开，分组行展开收起，条目开标签。
func (m *Model) chooseRow(i int) {
	if i < 0 || i >= len(m.rows) {
		return
	}
	row := m.rows[i]
	m.selKey = row.key
	switch row.kind {
	case nodeRoot:
		if !row.expanded {
			m.expanded[row.key] = true
			m.treeDirty = true
		}
		if m.active != row.root {
			m.activateRoot(row.root)
		}
		m.syncTree()
		m.selectRootRow(row.root)
	case nodeFolder:
		m.toggleRow(i)
	default:
		m.chooseEntryRow(row)
	}
}

// chooseEntryRow 打开一条内容：在别的集合根下就先切过去，再开标签。
func (m *Model) chooseEntryRow(row treeRow) {
	if m.active != row.root {
		m.activateRoot(row.root)
	}
	entry := row.entry
	if fresh := m.entryByFile(entry.File); fresh != nil {
		entry = fresh
	}
	m.openEntry(entry)
}

// rowOfEntry 找当前集合里某条目的可见行；不可见（被过滤掉）返回 -1。
func (m *Model) rowOfEntry(file string) int {
	for i := range m.rows {
		row := &m.rows[i]
		if row.kind == nodeEntry && row.root == m.active && row.entry.File == file {
			return i
		}
	}
	return -1
}

// rowOfRoot 找某个集合根的可见行。
func (m *Model) rowOfRoot(index int) int {
	for i := range m.rows {
		row := &m.rows[i]
		if row.kind == nodeRoot && row.root == index {
			return i
		}
	}
	return -1
}

// selectEntryRow 让树选中某条内容；条目不在可见行里就清掉选择。
func (m *Model) selectEntryRow(file string) {
	m.selectFile, m.selectPending = file, true
	m.selKey = "entry:" + file
	m.applySelect()
}

// applySelect 把待选中的条目落到树上：Tree 的行数要等这一帧画完才更新，
// 新行第一次 Select 会被截断，所以下一帧再试一次。
func (m *Model) applySelect() {
	if !m.selectPending {
		return
	}
	index := m.rowOfEntry(m.selectFile)
	if index < 0 {
		m.tree.Select(-1)
		m.selectPending = false
		return
	}
	m.tree.Select(index)
	if m.tree.Selected() != index {
		m.invalidator.Invalidate()
		return
	}
	m.selectPending = false
}

// selectRootRow 让树选中某个集合根。
func (m *Model) selectRootRow(root int) {
	index := m.rowOfRoot(root)
	if index >= 0 {
		m.tree.Select(index)
		m.selKey = m.rows[index].key
	}
}

// selectedRow 是树当前选中的行；没有选中返回 nil。
func (m *Model) selectedRow() *treeRow {
	index := m.tree.Selected()
	if index < 0 || index >= len(m.rows) {
		return nil
	}
	return &m.rows[index]
}

// treeContent 画一行：集合根带「更多」菜单，分组只有名字，条目是方法 + 名称。
func (m *Model) treeContent(i int) {
	if i < 0 || i >= len(m.rows) {
		return
	}
	c := m.sideC
	row := &m.rows[i]
	switch row.kind {
	case nodeRoot:
		m.rootRow(c, row, i)
	case nodeFolder:
		name := ui.Text(c, row.name).FontSize(13).Ellipsis().LabelFor(m.tree.RowID(i))
		folder := row.folder
		ui.ContextMenu(c, name, func(menu *ui.MenuItems) {
			if menu.Item("新建请求").Icon(IconPlus).Clicked() {
				m.openNewRequestIn(folder.Dir)
			}
			if menu.Item("新建文档").Icon(IconFilePlus).Clicked() {
				c.After(m.createDoc)
			}
		})
	default:
		m.entryRow(c, row, i)
	}
}

// rootRow 是集合根：名字 + 右侧「更多」（新建请求 / 新建文档 / 关闭集合）。
func (m *Model) rootRow(c ui.C, row *treeRow, i int) {
	root := row.root
	ui.Text(c, row.name).FontSize(13).Bold().Ellipsis().LabelFor(m.tree.RowID(i))
	ui.Spacer(c)
	more := ui.Button(c, "更多 "+row.name).Icon(IconEllipsis).IconOnly().Ghost()
	if more.Clicked() {
		c.After(func() {
			m.menuRoot = root
			m.menuOpen = true
		})
	}
	if m.menuRoot != root {
		return
	}
	ui.Menu(c, more, &m.menuOpen, func(menu *ui.MenuItems) {
		if menu.Item("新建请求").Icon(IconPlus).Clicked() {
			c.After(func() { m.createRequestIn(root) })
		}
		if menu.Item("新建文档").Icon(IconFilePlus).Clicked() {
			c.After(func() { m.createDocIn(root) })
		}
		menu.Separator()
		closeItem := menu.Item("关闭集合")
		if len(m.roots) <= 1 {
			closeItem.Disabled(true)
		}
		if closeItem.Clicked() {
			c.After(func() { m.closeRoot(root) })
		}
	})
}

// entryRow 是请求 / gRPC / 文档：方法色块 + 名称，右键菜单沿用集合树的操作。
func (m *Model) entryRow(c ui.C, row *treeRow, i int) {
	entry := row.entry
	ui.Text(c, methodText(entry)).FontSize(11).Mono().Bold().Width(46).
		Color(entryTint(&m.theme, entry, m.isLight()))
	name := ui.Text(c, entry.Name).FontSize(13).Ellipsis().LabelFor(m.tree.RowID(i))
	ui.ContextMenu(c, name, func(menu *ui.MenuItems) {
		if menu.Item("新建请求").Icon(IconPlus).Clicked() {
			m.selectEntryRow(entry.File)
			m.openNewRequest()
		}
		if menu.Item("新建文档").Icon(IconFilePlus).Clicked() {
			c.After(m.createDoc)
		}
		if menu.Item("重命名").Clicked() {
			m.openRenameEntry(entry)
		}
		if menu.Item("删除到回收站").Icon(IconTrash).Destructive().Clicked() {
			file := entry.File
			c.After(func() { m.trashEntry(file) })
		}
	})
}

// methodText 是树里方法列的短标签。
func methodText(entry *explorer.Entry) string {
	switch entry.Kind {
	case explorer.KindDoc:
		return "MD"
	case explorer.KindGRPC:
		return "gRPC"
	}
	if entry.Method == "" {
		return "HTTP"
	}
	return strings.ToUpper(entry.Method)
}

// 方法色取自参考图的令牌（--app-method-* / --app-md），浅暗各一套。
var (
	getLight, getDark     = ui.RGB(0x18, 0xa0, 0x58), ui.RGB(0x36, 0xad, 0x6a)
	postLight, postDark   = ui.RGB(0x20, 0x80, 0xf0), ui.RGB(0x40, 0x98, 0xfc)
	putLight, putDark     = ui.RGB(0xf0, 0xa0, 0x20), ui.RGB(0xf2, 0xc9, 0x7d)
	patchLight, patchDark = ui.RGB(0x8a, 0x2b, 0xe2), ui.RGB(0xc7, 0x92, 0xea)
	delLight, delDark     = ui.RGB(0xd0, 0x30, 0x50), ui.RGB(0xe8, 0x80, 0x80)
	grpcLight, grpcDark   = ui.RGB(0x0e, 0x94, 0xa8), ui.RGB(0x4c, 0xc9, 0xd8)
	mdLight, mdDark       = ui.RGB(0x00, 0x78, 0xd4), ui.RGB(0x4a, 0xa3, 0xe8)
	otherLight, otherDark = ui.RGB(0x8a, 0x91, 0x99), ui.RGB(0x9a, 0xa0, 0xa6)
)

func tone(light, dark ui.Color, isLight bool) ui.Color {
	if isLight {
		return light
	}
	return dark
}

func entryTint(theme *ui.Theme, entry *explorer.Entry, light bool) ui.Color {
	if entry.Kind == explorer.KindDoc {
		return tone(mdLight, mdDark, light)
	}
	if entry.Kind == explorer.KindGRPC {
		return tone(grpcLight, grpcDark, light)
	}
	switch methodText(entry) {
	case "GET", "HEAD", "OPTIONS":
		return tone(getLight, getDark, light)
	case "POST":
		return tone(postLight, postDark, light)
	case "PUT":
		return tone(putLight, putDark, light)
	case "PATCH":
		return tone(patchLight, patchDark, light)
	case "DELETE":
		return tone(delLight, delDark, light)
	default:
		return tone(otherLight, otherDark, light)
	}
}

// createRequestIn 在某个集合根里新建请求：先切过去，再开弹窗。
func (m *Model) createRequestIn(root int) {
	if root != m.active && root >= 0 && root < len(m.roots) {
		m.activateRoot(root)
	}
	m.openNewRequest()
}

// createDocIn 在某个集合根里新建文档：先切过去，再落盘。
func (m *Model) createDocIn(root int) {
	if root != m.active && root >= 0 && root < len(m.roots) {
		m.activateRoot(root)
	}
	m.createDoc()
}
