package view

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
)

// TestRunnerRetriesFailedOnly 跑完一批后，「重跑失败项」只重选失败的请求再跑一次。
func TestRunnerRetriesFailedOnly(t *testing.T) {
	var goneHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/gone") {
			// 第一次 500（制造失败），重跑那次 200。
			if goneHits.Add(1) == 1 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	col, err := explorer.Load(writeCollection(t, srv.URL))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)

	model.openRunner()
	pump(t, driver)
	if !model.ui.runOpen {
		t.Fatalf("运行器没打开：%v", painted(driver))
	}
	click(t, driver, "运行")
	waitFor(t, driver, "失败 1 条")

	if len(model.run.FailedFiles) != 1 || model.run.FailedFiles[0] != "删除.yml" {
		t.Fatalf("失败列表不对：%+v", model.run.FailedFiles)
	}
	if !hasControl(driver, "重跑失败项（1）") {
		t.Fatalf("应有「重跑失败项（1）」按钮：%v", painted(driver))
	}

	click(t, driver, "重跑失败项（1）")
	waitFor(t, driver, "全部通过（1 条）")

	if len(model.run.Results) != 1 {
		t.Fatalf("重跑只该有失败那条的结果：%+v", model.run.Results)
	}
	if model.run.Results[0].Name != "删除" || model.run.Results[0].Failed {
		t.Fatalf("重跑结果不对：%+v", model.run.Results[0])
	}
	if len(model.run.FailedFiles) != 0 {
		t.Fatalf("重跑通过后失败列表应清空：%+v", model.run.FailedFiles)
	}
	// 勾选状态跟着变成「只剩失败项」：手动再点运行也只跑这一条。
	selected := 0
	for _, row := range model.run.Rows {
		if row.Selected {
			selected++
		}
	}
	if selected != 1 {
		t.Fatalf("重跑后勾选应只剩 1 条：%d", selected)
	}
	if goneHits.Load() != 2 {
		t.Fatalf("/gone 应被请求两次（一次失败 + 一次重跑）：%d", goneHits.Load())
	}
}
