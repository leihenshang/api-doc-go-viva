package view

import (
	"path"
	"sort"
	"strings"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/vivaui/viva/gfx"
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
		key := dirKey(m.roots[root].col.Dir, folder.Dir)
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
			key:   entryKey(m.roots[root].col.Dir, entry.File),
			label: entry.Title(),
			name:  entry.Name,
			root:  root,
			entry: entry,
		})
	}
}

// dirKey / entryKey 是树行键：带上集合根目录，两个集合里同名分组 / 条目不会撞键
// （Viva 的 Tree 要求一次绘制内键唯一，否则会 panic）。
func dirKey(rootDir, dir string) string { return "dir:" + rootDir + "|" + dir }

func entryKey(rootDir, file string) string { return "entry:" + rootDir + "|" + file }

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
	m.selKey = entryKey(m.col.Dir, file)
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
	// 缩进引导线：每行放一个零宽锚点，按行首与层级在祖先列上画 1px 竖线。
	if row.depth > 0 {
		depth, branch := row.depth, row.branch
		ui.Slot(c).Width(1).Height(m.tree.RowHeight).
			Draw(func(d *ui.DrawContext) { m.drawTreeGuides(d, depth, branch) })
	}
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
			if menu.Item("新建分组").Icon(IconFolderOpen).Clicked() {
				c.After(func() { m.openNewFolderIn(folder.Dir) })
			}
			if menu.Item("导入 cURL…").Icon(IconImport).Clicked() {
				c.After(func() {
					m.activateFolderRoot(row.root)
					m.openCurlImport(folder.Dir)
				})
			}
			menu.Separator()
			if menu.Item("重命名").Clicked() {
				c.After(func() { m.openRenameFolder(folder) })
			}
			if menu.Item("克隆分组").Icon(IconCopy).Clicked() {
				c.After(func() {
					m.activateFolderRoot(row.root)
					m.cloneFolder(folder)
				})
			}
			if menu.Item("删除到回收站").Icon(IconTrash).Destructive().Clicked() {
				c.After(func() {
					m.activateFolderRoot(row.root)
					m.askDeleteFolder(folder)
				})
			}
		})
	default:
		m.entryRow(c, row, i)
	}
}

// drawTreeGuides 在祖先列上画竖线：锚点 X 是内容起点，减去缩进（分组行还要减去
// 24px 的箭头位）就是行首，再按层级往右推。
func (m *Model) drawTreeGuides(d *ui.DrawContext, depth int, branch bool) {
	indent := m.tree.Indent
	base := d.Rect.X - float32(depth)*indent
	if branch {
		base -= 24
	}
	color := m.pal.border
	for level := 1; level < depth; level++ {
		x := base + float32(level)*indent + 9
		d.Canvas.Fill(gfx.Rect{X: x, Y: d.Rect.Y, Width: 1, Height: m.tree.RowHeight}, color)
	}
}

// activateFolderRoot 让操作落在分组所属的集合根上（菜单可能开在非活动的根里）。
func (m *Model) activateFolderRoot(root int) {
	if root != m.active && root >= 0 && root < len(m.roots) {
		m.activateRoot(root)
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
		if menu.Item("新建分组").Icon(IconFolderOpen).Clicked() {
			c.After(func() {
				if m.active != root && root >= 0 && root < len(m.roots) {
					m.activateRoot(root)
				}
				m.openNewFolderIn("")
			})
		}
		if menu.Item("导入 cURL…").Icon(IconImport).Clicked() {
			c.After(func() {
				if m.active != root && root >= 0 && root < len(m.roots) {
					m.activateRoot(root)
				}
				m.openCurlImport("")
			})
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
	// 行尾「···」只在悬停时出现，入口与右键菜单一致（参考 03）。
	if canvas := c.Canvas(); canvas != nil && canvas.Hovered() == m.tree.RowID(i) {
		ui.Spacer(c)
		more := ui.Button(c, "更多 "+entry.Name).Icon(IconEllipsis).IconOnly().Ghost()
		if more.Clicked() {
			c.After(func() {
				m.menuEntry = entry.File
				m.menuOpen = true
			})
		}
		if m.menuEntry == entry.File {
			ui.Menu(c, more, &m.menuOpen, func(menu *ui.MenuItems) {
				if menu.Item("新建请求").Icon(IconPlus).Clicked() {
					m.selectEntryRow(entry.File)
					m.openNewRequest()
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
	}
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

// dropRow 处理拖放：落到分组里就换上级，落到条目前后就排到同级的新位置。
func (m *Model) dropRow(move ui.TreeMove) {
	if move.From < 0 || move.From >= len(m.rows) || move.To < 0 || move.To >= len(m.rows) {
		return
	}
	row := m.rows[move.From]
	switch row.kind {
	case nodeEntry:
		if move.Where == ui.TreeDropInside {
			target, ok := m.folderTarget(move.To)
			if !ok {
				return
			}
			m.dropEntry(row.root, move.To, row.entry, target)
			return
		}
		m.arrangeEntry(row.entry, move)
	case nodeFolder:
		if move.Where == ui.TreeDropInside {
			target, ok := m.folderTarget(move.To)
			if !ok {
				return
			}
			m.dropFolder(row.root, move.To, row.folder, target)
			return
		}
		m.arrangeFolder(row.folder, move)
	}
}

// dropEntry 按源 / 目标是否同一个集合根，走集合内移动或跨集合移动。
func (m *Model) dropEntry(srcRoot, to int, entry *explorer.Entry, folder string) {
	dstRoot := m.rows[to].root
	if srcRoot == dstRoot {
		m.moveEntry(entry.File, folder)
		return
	}
	moved, err := explorer.MoveEntryTo(m.roots[srcRoot].col.Dir, m.roots[dstRoot].col.Dir, entry, folder)
	if err != nil {
		m.createErr = err.Error()
		m.invalidator.Invalidate()
		return
	}
	m.afterCrossMove(srcRoot, dstRoot, folder, moved, false)
}

// dropFolder 是分组版的跨集合落点。
func (m *Model) dropFolder(srcRoot, to int, folder *explorer.Folder, parent string) {
	dstRoot := m.rows[to].root
	if srcRoot == dstRoot {
		m.moveFolder(folder, parent)
		return
	}
	moved, err := explorer.MoveFolderTo(m.roots[srcRoot].col.Dir, m.roots[dstRoot].col.Dir, folder, parent)
	if err != nil {
		m.createErr = err.Error()
		m.invalidator.Invalidate()
		return
	}
	m.afterCrossMove(srcRoot, dstRoot, parent, moved, true)
}

// afterCrossMove 重扫源 / 目标两个根；活动根是目标就选中移过去的那一项，
// 是源就把已经离开的选中项清掉。源根的标签由 refreshTabs 自动丢弃。
func (m *Model) afterCrossMove(srcRoot, dstRoot int, parent, moved string, folder bool) {
	m.reloadRoot(srcRoot)
	m.reloadRoot(dstRoot)
	if dstRoot == m.active {
		m.expandDir(parent)
		if folder {
			m.selectFolderDir(moved)
		} else {
			m.selectEntryRow(moved)
		}
	} else if srcRoot == m.active {
		m.tree.Select(-1)
		m.selKey = ""
		m.pendingKey = ""
		m.pendingSelect = ""
	}
	m.invalidator.Invalidate()
}

// reloadRoot 重扫第 i 个集合根；活动的那个顺带刷新列表与标签。
func (m *Model) reloadRoot(i int) {
	if i < 0 || i >= len(m.roots) {
		return
	}
	col, err := explorer.Load(m.roots[i].col.Dir)
	if err != nil {
		m.createErr = err.Error()
		return
	}
	m.roots[i].col = col
	m.treeDirty = true
	if i != m.active {
		return
	}
	m.col = col
	m.shownOnce = false
	m.refreshShown()
	m.refreshTabs()
}

// arrangeFolder 把分组排到同父目录另一个分组的前 / 后，只重写各分组 folder.yml 的 seq。
func (m *Model) arrangeFolder(folder *explorer.Folder, move ui.TreeMove) {
	target := m.rows[move.To].folder
	if target == nil {
		return
	}
	dirs := make([]string, 0, len(m.col.Folders))
	for _, sibling := range m.siblingFolders(parentDir(folder.Dir)) {
		if sibling != folder.Dir {
			dirs = append(dirs, sibling)
		}
	}
	at := len(dirs)
	for index, dir := range dirs {
		if dir == target.Dir {
			at = index
			if move.Where == ui.TreeDropAfter {
				at = index + 1
			}
			break
		}
	}
	dirs = append(dirs, "")
	copy(dirs[at+1:], dirs[at:])
	dirs[at] = folder.Dir

	if err := explorer.ArrangeFolders(m.col.Dir, dirs); err != nil {
		m.createErr = err.Error()
		m.invalidator.Invalidate()
		return
	}
	if m.reloadCollection() {
		m.selectFolderDir(folder.Dir)
		m.invalidator.Invalidate()
	}
}

// siblingFolders 按树的同层顺序列出某个父目录下的分组（seq 再按名称）。
func (m *Model) siblingFolders(parent string) []string {
	list := make([]*explorer.Folder, 0, len(m.col.Folders))
	for _, folder := range m.col.Folders {
		if parentDir(folder.Dir) == parent {
			list = append(list, folder)
		}
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Seq != list[j].Seq {
			return list[i].Seq < list[j].Seq
		}
		return list[i].Name < list[j].Name
	})
	dirs := make([]string, 0, len(list))
	for _, folder := range list {
		dirs = append(dirs, folder.Dir)
	}
	return dirs
}

// moveFolder 换父目录：目录整体改名，打开中的标签跟着旧前缀改成新前缀，保持打开。
func (m *Model) moveFolder(folder *explorer.Folder, parent string) {
	moved, err := explorer.MoveFolder(m.col.Dir, folder, parent)
	if err != nil {
		m.createErr = err.Error()
		m.invalidator.Invalidate()
		return
	}
	oldPrefix, newPrefix := folder.Dir+"/", moved+"/"
	// 当前草稿、选中项、待选中项与所有标签都存着旧路径：先一起换前缀，
	// 再重扫集合，refreshTabs 才能按新路径重新认回条目。
	m.expandDir(parent)
	m.entryFile = remapPath(m.entryFile, oldPrefix, newPrefix)
	m.req.file = remapPath(m.req.file, oldPrefix, newPrefix)
	m.pendingSelect = remapPath(m.pendingSelect, oldPrefix, newPrefix)
	m.selKey = remapKey(m.selKey, oldPrefix, newPrefix)
	for _, tab := range m.tabs {
		if tab.scratch {
			continue
		}
		tab.file = remapPath(tab.file, oldPrefix, newPrefix)
	}
	if m.reloadCollection() {
		m.selectFolderDir(moved)
		m.invalidator.Invalidate()
	}
}

// remapPath 把目录前缀换掉；不在这棵子树里的路径原样返回。
func remapPath(path, oldPrefix, newPrefix string) string {
	if strings.HasPrefix(path, oldPrefix) {
		return newPrefix + strings.TrimPrefix(path, oldPrefix)
	}
	return path
}

// remapKey 是 remapPath 的树行键版本（dir:<根>|<目录> / entry:<根>|<文件>）：
// 根目录前缀不动，只换 "|" 后面的相对路径。
func remapKey(key, oldPrefix, newPrefix string) string {
	for _, kind := range []string{"entry:", "dir:"} {
		if !strings.HasPrefix(key, kind) {
			continue
		}
		rest := strings.TrimPrefix(key, kind)
		if bar := strings.IndexByte(rest, '|'); bar >= 0 {
			return kind + rest[:bar+1] + remapPath(rest[bar+1:], oldPrefix, newPrefix)
		}
		return key
	}
	return key
}

// selectFolderDir 在下一帧选中某个分组行（标签与详情不动）。
func (m *Model) selectFolderDir(dir string) {
	m.pendingKey = dirKey(m.col.Dir, dir)
	m.selKey = m.pendingKey
	m.selectPending = false
}

// rowOfKey 按树行键找可见行。
func (m *Model) rowOfKey(key string) int {
	for i := range m.rows {
		if m.rows[i].key == key {
			return i
		}
	}
	return -1
}

// arrangeEntry 把一条内容排到目标条目的前 / 后：先把同目录的完整顺序算出来
// （含被过滤隐藏的条目），再把 seq 重排成 1..N。只重排同一目录，绝不换上级。
func (m *Model) arrangeEntry(entry *explorer.Entry, move ui.TreeMove) {
	target := m.rows[move.To].entry
	if target == nil {
		return
	}
	dir := requestDir(entry.File)
	ordered := make([]string, 0, len(m.col.Entries))
	for _, file := range m.siblingFiles(dir) {
		if file != entry.File {
			ordered = append(ordered, file)
		}
	}
	at := len(ordered)
	for index, file := range ordered {
		if file == target.File {
			at = index
			if move.Where == ui.TreeDropAfter {
				at = index + 1
			}
			break
		}
	}
	ordered = append(ordered, "")
	copy(ordered[at+1:], ordered[at:])
	ordered[at] = entry.File

	if err := explorer.ArrangeEntries(m.col.Dir, ordered); err != nil {
		m.createErr = err.Error()
		m.invalidator.Invalidate()
		return
	}
	if m.reloadCollection() {
		// 排序只换位置，不该顺手打开标签：选中它，等下一帧落回树上。
		m.selectEntryRow(entry.File)
		m.invalidator.Invalidate()
	}
}

// siblingFiles 按树的同层次顺序列出某目录里的全部内容（seq 再按名称）。
func (m *Model) siblingFiles(dir string) []string {
	list := make([]*explorer.Entry, 0, len(m.col.Entries))
	for _, entry := range m.col.Entries {
		if requestDir(entry.File) == dir {
			list = append(list, entry)
		}
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Seq != list[j].Seq {
			return list[i].Seq < list[j].Seq
		}
		return list[i].Name < list[j].Name
	})
	files := make([]string, 0, len(list))
	for _, entry := range list {
		files = append(files, entry.File)
	}
	return files
}

// moveEntry 移动条目并重扫：旧路径的标签关掉，新路径重新打开。
func (m *Model) moveEntry(file, folder string) {
	entry := m.entryByFile(file)
	if entry == nil {
		return
	}
	moved, err := explorer.MoveEntry(m.col.Dir, entry, folder)
	if err != nil {
		m.createErr = err.Error()
		m.invalidator.Invalidate()
		return
	}
	// 已经打开的标签跟着文件走，不额外弹新标签；草稿路径也一起换。
	for _, tab := range m.tabs {
		if !tab.scratch && tab.file == file {
			tab.file = moved
		}
	}
	if m.req.file == file {
		m.req.file = moved
	}
	if m.entryFile == file {
		m.entryFile = moved
	}
	if m.selKey == entryKey(m.col.Dir, file) {
		m.selKey = entryKey(m.col.Dir, moved)
	}
	// 落到折叠的分组里要把它展开，否则用户看不到移动结果。
	m.expandDir(folder)
	if m.reloadCollection() {
		m.selectEntryRow(moved)
	}
	m.invalidator.Invalidate()
}

// expandDir 展开某个分组（空串是集合根），只改展开状态、不碰选择。
func (m *Model) expandDir(dir string) {
	key := dirKey(m.col.Dir, dir)
	if dir == "" {
		key = "root:" + m.col.Dir
	}
	m.expanded[key] = true
	m.treeDirty = true
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
