package store

import (
	"path/filepath"
	"testing"

	"github.com/fushuqian/amane-lite/internal/model"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "amane.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func saveMeta(t *testing.T, st *Store, number, title string) {
	t.Helper()
	agg := model.Aggregated{MediaMetadata: model.MediaMetadata{Number: number, Title: title}}
	if err := st.SaveMetadata(agg, model.Censored, nil); err != nil {
		t.Fatal(err)
	}
}

func TestListMetadataOrderByCreatedAt(t *testing.T) {
	st := openTest(t)
	saveMeta(t, st, "AAA-001", "旧片")
	saveMeta(t, st, "BBB-002", "新片")
	// 重刮旧片只更新 updated_at，不应把它顶到最前。
	saveMeta(t, st, "AAA-001", "旧片重刮")

	items, total, err := st.ListMetadata(0, 10, "")
	if err != nil || total != 2 {
		t.Fatalf("total = %d, err = %v", total, err)
	}
	if items[0].Number != "BBB-002" || items[1].Number != "AAA-001" {
		t.Fatalf("顺序错误: %+v", items)
	}
}

func TestListActiveTasks(t *testing.T) {
	st := openTest(t)
	id, err := st.CreateTask("sync", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	task, err := st.ClaimTask()
	if err != nil || task == nil {
		t.Fatalf("claim: %v %v", task, err)
	}
	if active, _ := st.HasActive("sync"); !active {
		t.Fatal("运行中的 sync 应视为活跃")
	}
	if err := st.FinishTask(id, "done", "{}", ""); err != nil {
		t.Fatal(err)
	}
	if active, _ := st.HasActive("sync"); active {
		t.Fatal("完成后的 sync 不应再活跃")
	}
	active, err := st.ListActiveTasks(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("active = %+v", active)
	}
}

func TestUpdateMediaFilePath(t *testing.T) {
	st := openTest(t)
	info := model.FileInfo{Number: "MIDV-123", CD: 1}
	if err := st.RegisterMediaFile("/downloads/a.mp4", info, 10); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateMediaFilePath("/downloads/a.mp4", "/media/MIDV-123/a.mp4"); err != nil {
		t.Fatal(err)
	}
	files, err := st.ListMediaFiles("MIDV-123")
	if err != nil || len(files) != 1 {
		t.Fatalf("files = %+v, err = %v", files, err)
	}
	if files[0].Path != "/media/MIDV-123/a.mp4" || files[0].CD != 1 {
		t.Fatalf("row = %+v", files[0])
	}
}
