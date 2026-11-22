package view

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/leihenshang/api-doc-go-viva/internal/settings"
)

// requestNames 取树里条目行的名字（按可见顺序）。
// 树行是在绘制时按脏标记重建的，测试里显式重建一次。
func requestNames(m *Model) []string {
	m.rebuildTree()
	out := make([]string, 0, len(m.rows))
	for _, row := range m.rows {
		if row.kind == nodeEntry {
			out = append(out, row.name)
		}
	}
	return out
}

// writeOrdered 造一个「磁盘顺序与名称顺序不同」的集合：乙(1) 甲(2) 丙(3)。
func writeOrdered(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name string, seq int) {
		body := "info:\n  name: " + name + "\n  type: http\n  seq: " + itoa(seq) + "\n" +
			"meta:\n  uid: order-" + name + "\nhttp:\n  method: GET\n  url: http://127.0.0.1:1/" + name + "\n" +
			"  body:\n    type: none\n"
		if err := os.WriteFile(filepath.Join(dir, name+".yml"), []byte(body), 0o644); err != nil {
			t.Fatalf("写 %s: %v", name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "opencollection.yml"),
		[]byte("opencollection: 1.0.0\ninfo:\n  name: 排序集合\n"), 0o644); err != nil {
		t.Fatalf("写清单: %v", err)
	}
	write("乙", 1)
	write("甲", 2)
	write("丙", 3)
	return dir
}

// itoa 是个小助手，避免为了一个数字引入 strconv。
func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}

// TestSortModes 磁盘顺序 / 名称升降三种口径，目录不受影响。
func TestSortModes(t *testing.T) {
	col, err := explorer.Load(writeOrdered(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	// 排序是全局偏好：用例结束要还原，否则后面的拖拽用例会因「排序禁拖」而失败。
	t.Cleanup(func() { model.setSortMode("") })

	if got := requestNames(model); len(got) != 3 || got[0] != "乙" {
		t.Fatalf("磁盘顺序应按 seq：%v", got)
	}
	model.setSortMode("name-asc")
	if got := requestNames(model); got[0] != "丙" || got[1] != "乙" || got[2] != "甲" {
		t.Fatalf("名称升序不对：%v", got)
	}
	model.setSortMode("name-desc")
	if got := requestNames(model); got[0] != "甲" || got[2] != "丙" {
		t.Fatalf("名称降序不对：%v", got)
	}
	// 落到偏好里，重开还是这个顺序。
	if saved := settings.Load().SortMode; saved != "name-desc" {
		t.Fatalf("排序没落盘：%q", saved)
	}
	model.setSortMode("")
	if got := requestNames(model); got[0] != "乙" {
		t.Fatalf("切回磁盘顺序不对：%v", got)
	}
}

// TestFavoritesPinAndPersist 收藏排到最前、行上有标记、重开还在；再点一次取消。
func TestFavoritesPinAndPersist(t *testing.T) {
	dir := writeOrdered(t)
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)

	var target *explorer.Entry
	for _, entry := range col.Entries {
		if entry.Name == "丙" {
			target = entry
		}
	}
	if target == nil {
		t.Fatal("找不到「丙」")
	}
	model.toggleFavorite(target)
	if got := requestNames(model); got[0] != "丙" {
		t.Fatalf("收藏应排到最前：%v", got)
	}
	favRows := 0
	for _, row := range model.rows {
		if row.kind == nodeEntry && row.name == "丙" && row.fav {
			favRows++
		}
	}
	if favRows != 1 {
		t.Fatalf("行上应有收藏标记：%+v", model.rows)
	}
	if !model.isFav("丙.yml") {
		t.Fatal("内存里的收藏集合没有它")
	}

	// 换个集合根不串：另一个集合的收藏是空的。
	other, err := explorer.Load(writeOrdered(t))
	if err != nil {
		t.Fatalf("Load other: %v", err)
	}
	otherModel := New(other, "", nil)
	if otherModel.isFav("丙.yml") {
		t.Fatal("收藏不该跨集合")
	}

	// 重开（同一目录）还在。
	reopened, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load reopened: %v", err)
	}
	again := New(reopened, "", nil)
	if !again.isFav("丙.yml") {
		t.Fatal("重开后收藏丢了")
	}

	// 再点一次 = 取消，且从偏好里删干净。
	again.toggleFavorite(target)
	if again.isFav("丙.yml") {
		t.Fatal("取消收藏没生效")
	}
	if list := settings.Load().Favorites[dir]; len(list) != 0 {
		t.Fatalf("偏好里还留着：%v", list)
	}
}

// TestSortModeBlocksDrag 排序模式下不接拖拽：拖了顺序也不会变，别给假反馈。
func TestSortModeBlocksDrag(t *testing.T) {
	col, err := explorer.Load(writeOrdered(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	model.rebuildTree()

	// 先找一对在磁盘顺序下确实可拖的行（树结构决定谁和谁能拖，不能写死下标）。
	found := [2]int{-1, -1}
	for from := range model.rows {
		for to := range model.rows {
			if model.dropAllowed(from, to, 0) {
				found = [2]int{from, to}
			}
		}
	}
	if found[0] < 0 {
		t.Fatal("磁盘顺序下应至少存在一对可拖的行")
	}
	model.setSortMode("name-asc")
	t.Cleanup(func() { model.setSortMode("") })
	if model.dropAllowed(found[0], found[1], 0) {
		t.Fatalf("排序模式下不该接受拖拽：%v", found)
	}
}
