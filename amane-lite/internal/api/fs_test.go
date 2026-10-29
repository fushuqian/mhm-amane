package api

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestBrowseFS(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AMANE_SAFE_DIRS", root)
	h := (&Server{}).Handler()

	get := func(path string) (*httptest.ResponseRecorder, map[string]any) {
		target := "/api/fs"
		if path != "" {
			target += "?path=" + path
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", target, nil))
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		return rec, body
	}

	rec, body := get("")
	if rec.Code != 200 {
		t.Fatalf("空 path 状态 = %d", rec.Code)
	}
	if roots, _ := body["roots"].([]any); len(roots) != 1 || roots[0] != root {
		t.Fatalf("roots = %v", body["roots"])
	}

	rec, body = get(root)
	if rec.Code != 200 {
		t.Fatalf("列目录状态 = %d", rec.Code)
	}
	entries, _ := body["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("entries = %v", body["entries"])
	}

	rec, _ = get(filepath.Dir(root))
	if rec.Code != 403 {
		t.Fatalf("越界应返回 403，实际 %d", rec.Code)
	}
}
