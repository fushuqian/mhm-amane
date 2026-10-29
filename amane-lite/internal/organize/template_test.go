package organize

import (
	"testing"

	"github.com/fushuqian/amane-lite/internal/model"
	"github.com/fushuqian/amane-lite/internal/store"
)

func baseRec() *store.MetaRecord {
	return &store.MetaRecord{
		Number:      "MIDV-123",
		ContentType: model.Censored,
		Title:       "标题",
		Release:     "2025-01-02",
		Actors:      []model.FilmActor{{Name: "三田サクラ", Gender: model.GenderFemale}},
	}
}

func TestRenderDefaultTemplate(t *testing.T) {
	const tmpl = "{number}[-CD{cd?}][-{sub?}].{ext}"
	cases := []struct {
		file store.MediaFileRow
		want string
	}{
		{store.MediaFileRow{Path: "/downloads/MIDV-123.mp4"}, "MIDV-123.mp4"},
		{store.MediaFileRow{Path: "/downloads/MIDV-123-CD1.mkv", CD: 1}, "MIDV-123-CD1.mkv"},
		{store.MediaFileRow{Path: "/downloads/MIDV-123-C.mp4", HasSubtitle: true}, "MIDV-123-C.mp4"},
		{store.MediaFileRow{Path: "/downloads/MIDV-123-CD2-C.mp4", CD: 2, HasSubtitle: true}, "MIDV-123-CD2-C.mp4"},
	}
	for _, c := range cases {
		got, err := Render(tmpl, buildValues(baseRec(), c.file))
		if err != nil {
			t.Fatalf("render %s: %v", c.file.Path, err)
		}
		if got != c.want {
			t.Fatalf("render %s = %q, want %q", c.file.Path, got, c.want)
		}
	}
}

func TestRenderDoubleGroup(t *testing.T) {
	vals := buildValues(baseRec(), store.MediaFileRow{Path: "/downloads/MIDV-123.mp4"})
	got, err := Render("[[{def?}]]", vals)
	if err != nil || got != "" {
		t.Fatalf("empty def = %q, err = %v", got, err)
	}
	vals["def?"] = "1080p"
	if got, _ := Render("[[{def?}]]", vals); got != "1080p" {
		t.Fatalf("double group = %q", got)
	}
}

func TestRenderMapping(t *testing.T) {
	vals := buildValues(baseRec(), store.MediaFileRow{Path: "/downloads/MIDV-123.mp4"})
	vals["mosaic?"] = "censored"
	if got, _ := Render("{mosaic?|censored=有码,uncensored=无码}", vals); got != "有码" {
		t.Fatalf("mapping = %q", got)
	}
	vals["mosaic?"] = "leaked"
	if got, _ := Render("{mosaic?|censored=有码,uncensored=无码}", vals); got != "leaked" {
		t.Fatalf("unmapped = %q", got)
	}
}

func TestRenderUnknownPlaceholder(t *testing.T) {
	got, err := Render("{whatever}", map[string]string{})
	if err != nil || got != "Unknown" {
		t.Fatalf("unknown = %q, err = %v", got, err)
	}
	if err := ValidateTemplate("{whatever}"); err == nil {
		t.Fatal("未知占位符应当被拒绝")
	}
}

func TestValidateTemplate(t *testing.T) {
	good := []string{
		"{actress}/{number}[-CD{cd?}][-{sub?}].{ext}",
		"[[{def?}]]{number}",
		"{mosaic?|censored=有码,uncensored=无码}/{number}",
	}
	for _, tmpl := range good {
		if err := ValidateTemplate(tmpl); err != nil {
			t.Fatalf("template %q: %v", tmpl, err)
		}
	}
	bad := []string{"", "/{number}.{ext}", "{number", "{number|a}"}
	for _, tmpl := range bad {
		if err := ValidateTemplate(tmpl); err == nil {
			t.Fatalf("template %q 应当被拒绝", tmpl)
		}
	}
}

func TestCleanRelPath(t *testing.T) {
	if got, err := CleanRelPath("演员A/MIDV-123.mp4"); err != nil || got != "演员A/MIDV-123.mp4" {
		t.Fatalf("clean = %q, err = %v", got, err)
	}
	if _, err := CleanRelPath("../../etc/passwd"); err == nil {
		t.Fatal(".. 应当被拒绝")
	}
	if _, err := CleanRelPath("/media/x"); err == nil {
		t.Fatal("绝对路径应当被拒绝")
	}
}

func TestRenderVideoPathAddsExtension(t *testing.T) {
	file := store.MediaFileRow{Path: "/downloads/MIDV-123.mkv"}
	got, err := RenderVideoPath("{actress}/{number}", baseRec(), file)
	if err != nil {
		t.Fatal(err)
	}
	if got != "三田サクラ/MIDV-123.mkv" {
		t.Fatalf("path = %q", got)
	}
}
