package runner

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
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

// TestRetryRecoversFailure 单条失败后重试一次：只有最终结果进 results，
// 重试不改变量袋语义（同一条请求用开跑前的变量快照）。
func TestRetryRecoversFailure(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	plan := send.Plan{File: requestFile("GET", srv.URL), MaxBody: 1 << 20}
	cases := []Case{{Name: "偶发", File: "偶发.yml", Plan: plan}}

	results := Run(context.Background(), cases, Options{Retry: 1}, nil)
	if len(results) != 1 {
		t.Fatalf("重试不该多出结果条数：%d", len(results))
	}
	if results[0].Failed || results[0].Status != http.StatusOK {
		t.Fatalf("重试后应通过：%+v", results[0])
	}
	if hits.Load() != 2 {
		t.Fatalf("应请求两次：%d", hits.Load())
	}

	hits.Store(0)
	results = Run(context.Background(), cases, Options{}, nil)
	if !results[0].Failed {
		t.Fatalf("不重试时第一次就失败：%+v", results[0])
	}
}
