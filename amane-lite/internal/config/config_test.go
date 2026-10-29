package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeDefaults(t *testing.T) {
	cfg := Default("/media", "/tmp/missing")
	cfg.Library.VideoTemplate = ""
	cfg.Automation.IntervalMinutes = 0
	cfg.DownloadDirs = []DownloadDir{{Path: "/downloads", Mode: "hardlink"}, {Path: "/downloads", Mode: "copy"}}
	cfg.Normalize()

	if cfg.Library.VideoTemplate != DefaultVideoTemplate {
		t.Fatalf("template = %q", cfg.Library.VideoTemplate)
	}
	if cfg.Automation.IntervalMinutes != 30 {
		t.Fatalf("interval = %d", cfg.Automation.IntervalMinutes)
	}
	if len(cfg.DownloadDirs) != 1 || cfg.DownloadDirs[0].Mode != ModeCopy {
		t.Fatalf("download dirs = %+v", cfg.DownloadDirs)
	}
}

func TestStripRootPrefix(t *testing.T) {
	if got := StripRootPrefix("/media", "/media/{actress}/{number}.{ext}"); got != "{actress}/{number}.{ext}" {
		t.Fatalf("strip = %q", got)
	}
	if got := StripRootPrefix("/media", "{actress}/{number}.{ext}"); got != "{actress}/{number}.{ext}" {
		t.Fatalf("strip = %q", got)
	}
}

func TestValidateRejectsSamePath(t *testing.T) {
	cfg := Default("/media", "/tmp/missing")
	cfg.DownloadDirs = []DownloadDir{{Path: "/media", Mode: ModeCopy}}
	cfg.Normalize()
	if err := cfg.Validate(); err == nil {
		t.Fatal("下载目录与片库相同应当报错")
	}
}

func TestLoadMigratesLegacy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	raw := map[string]any{
		"addr":       "127.0.0.1:8088",
		"proxy":      "http://127.0.0.1:7890",
		"media_root": "/media",
		"sites":      []string{"javbus"},
		"library": map[string]any{
			"root":          "/media",
			"mode":          "hardlink",
			"dir_template":  "{actress}",
			"file_template": "{number}",
			"write_nfo":     true,
			"write_images":  true,
		},
	}
	data, _ := json.Marshal(raw)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path, "/media", "/tmp/missing")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Library.VideoTemplate != "{actress}/{number}.{ext}" {
		t.Fatalf("migrated template = %q", cfg.Library.VideoTemplate)
	}
	if !cfg.UseMihomo {
		t.Fatal("旧 proxy 应迁移为 mihomo 开关")
	}
}
