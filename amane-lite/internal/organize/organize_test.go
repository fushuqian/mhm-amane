package organize

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fushuqian/amane-lite/internal/model"
	"github.com/fushuqian/amane-lite/internal/store"
)

func TestExecuteCopiesVideoAndWritesNFO(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "incoming", "MIDV-123.mp4")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}

	st, err := store.Open(filepath.Join(tmp, "amane.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	info := model.FileInfo{Number: "MIDV-123", ContentType: model.Censored, Definition: "1080p"}
	if err := st.RegisterMediaFile(src, info, 5); err != nil {
		t.Fatal(err)
	}
	agg := model.Aggregated{MediaMetadata: model.MediaMetadata{
		Number: "MIDV-123", Title: "标题 & <演示>", Studio: "S1", Release: "2025-01-02",
		Runtime: 120, Tags: []string{"标签"}, Actors: []model.FilmActor{{Name: "演员A", Gender: model.GenderFemale}},
	}}
	if err := st.SaveMetadata(agg, model.Censored, nil); err != nil {
		t.Fatal(err)
	}
	rec, err := st.GetMetadata("MIDV-123")
	if err != nil {
		t.Fatal(err)
	}
	files, err := st.ListMediaFiles("MIDV-123")
	if err != nil || len(files) != 1 {
		t.Fatalf("files = %v, err = %v", files, err)
	}

	lib := Options{Root: filepath.Join(tmp, "library"), Mode: "copy",
		VideoTemplate: "{number}/{number}.{ext}", WriteNFO: true}
	result, err := Execute(context.Background(), rec, files, lib, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("errors = %v", result.Errors)
	}
	target := filepath.Join(tmp, "library", "MIDV-123", "MIDV-123.mp4")
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("target missing: %v", err)
	}
	nfo, err := os.ReadFile(filepath.Join(tmp, "library", "MIDV-123", "MIDV-123.nfo"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(nfo), "标题 &amp; &lt;演示&gt;") {
		t.Fatalf("nfo not escaped: %s", nfo)
	}
	if !strings.Contains(string(nfo), "<num>MIDV-123</num>") {
		t.Fatalf("nfo missing num: %s", nfo)
	}
}

func TestSanitize(t *testing.T) {
	cases := map[string]string{
		`AC/DC: "x"`: "AC DC x",
		`.. / ..`:    "",
	}
	for in, want := range cases {
		if got := Sanitize(in); got != want {
			t.Fatalf("Sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}
