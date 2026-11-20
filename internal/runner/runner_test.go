package runner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/leihenshang/api-doc-go-share/collection"
	"github.com/leihenshang/api-doc-go-viva/internal/send"
)

// caseFor 造一条打到 target 的 GET。
func caseFor(name, target string) Case {
	return Case{Name: name, File: name + ".yml", Plan: send.Plan{
		File: &collection.RequestFile{
			Info: collection.FileInfo{Name: name, Type: "http"},
			HTTP: collection.HTTPBlock{Method: "GET", URL: target},
		},
	}}
}

// TestRunReportsEveryCase 顺序跑完每一条：状态码与耗时都记下来，失败按 >=400 判。
func TestRunReportsEveryCase(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bad" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	var reported []string
	cases := []Case{caseFor("好", srv.URL+"/good"), caseFor("坏", srv.URL+"/bad")}
	results := Run(context.Background(), cases, Options{}, func(result Result) {
		reported = append(reported, result.Name)
	})
	if len(results) != 2 || len(reported) != 2 {
		t.Fatalf("两条都该跑：%+v %v", results, reported)
	}
	if results[0].Status != 200 || results[0].Failed {
		t.Fatalf("第一条应成功：%+v", results[0])
	}
	if results[1].Status != 500 || !results[1].Failed {
		t.Fatalf("第二条应失败：%+v", results[1])
	}
	if results[0].Duration <= 0 {
		t.Fatalf("应记下耗时：%+v", results[0])
	}
}

// TestRunStopsOnFailure 勾了「失败即停」就只跑一条。
func TestRunStopsOnFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	cases := []Case{caseFor("坏1", srv.URL+"/a"), caseFor("坏2", srv.URL+"/b")}
	results := Run(context.Background(), cases, Options{StopOnFailure: true}, nil)
	if len(results) != 1 {
		t.Fatalf("应当在第一条失败后停下：%+v", results)
	}
}

// TestRunStopsWhenCanceled 取消后不再发下一条。
func TestRunStopsWhenCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	results := Run(ctx, []Case{caseFor("一", srv.URL), caseFor("二", srv.URL)}, Options{}, nil)
	if len(results) != 0 {
		t.Fatalf("取消后不该再发：%+v", results)
	}
}

// TestRunRecordsTransportErrors 连不上也要记一条失败，而不是静默跳过。
func TestRunRecordsTransportErrors(t *testing.T) {
	results := Run(context.Background(), []Case{caseFor("断网", "http://127.0.0.1:1/x")}, Options{}, nil)
	if len(results) != 1 || !results[0].Failed || results[0].Err == "" {
		t.Fatalf("应记一条传输错误：%+v", results)
	}
}
