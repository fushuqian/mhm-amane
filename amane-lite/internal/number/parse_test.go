package number

import (
	"testing"

	"github.com/fushuqian/amane-lite/internal/model"
)

func TestParseText(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		num   string
		ctype model.ContentType
	}{
		{"标准番号", "MIDV-123", "MIDV-123", model.Censored},
		{"大小写", "midv-123", "MIDV-123", model.Censored},
		{"FC2 连写", "FC2PPV-1234567", "FC2-1234567", model.FC2},
		{"HEYZO", "HEYZO-1234", "HEYZO-1234", model.Uncensored},
		{"hey douga", "heydouga-4037-1002", "HEYDOUGA-4037-1002", model.Uncensored},
		{"素人补数字段", "ARA-423", "261ARA-423", model.Amateur},
		{"六位日期号", "010115-001", "010115-001", model.Uncensored},
		{"数字前缀番号", "R18-292", "R18-292", model.Censored},
		{"TMA 连写", "T2800123", "T28-123", model.Censored},
		{"东京热", "k1363", "k1363", model.Uncensored},
		{"无码前缀", "SKYHD-123", "SKYHD-123", model.Uncensored},
		{"未命中不冒充", "a random title", "", model.Western},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info := ParseText(tc.in, nil)
			if info.Number != tc.num {
				t.Fatalf("number = %q, want %q", info.Number, tc.num)
			}
			if info.ContentType != tc.ctype {
				t.Fatalf("content_type = %q, want %q", info.ContentType, tc.ctype)
			}
		})
	}
}

func TestParsePath(t *testing.T) {
	info := ParsePath(`C:\media\無碼\ABC-123-CD1-1080p.mp4`, nil)
	if info.Number != "ABC-123" {
		t.Fatalf("number = %q", info.Number)
	}
	if info.CD != 1 {
		t.Fatalf("cd = %d", info.CD)
	}
	if info.Mosaic != model.MosaicUncensored {
		t.Fatalf("mosaic = %q", info.Mosaic)
	}
	if info.Definition != "1080p" {
		t.Fatalf("definition = %q", info.Definition)
	}
	// 目录里的「無碼」是文件标记，不是条目类型；ABC-123 的类型仍按番号判定为有码。
	if info.ContentType != model.Censored {
		t.Fatalf("content_type = %q", info.ContentType)
	}
}

func TestParsePathFallbackParentDir(t *testing.T) {
	path := `/media/里番/ABC-123/random.mp4`
	info := ParsePath(path, nil)
	if info.Number != "ABC-123" {
		t.Fatalf("number = %q", info.Number)
	}
	if info.ContentType != model.Hentai {
		t.Fatalf("content_type = %q", info.ContentType)
	}
}

func TestParsePathSubtitleAndCrack(t *testing.T) {
	info := ParsePath(`/media/ABC-123-UC.mp4`, nil)
	if !info.HasSubtitleInName {
		t.Fatal("want subtitle flag")
	}
	if info.Mosaic != model.MosaicCracked {
		t.Fatalf("mosaic = %q", info.Mosaic)
	}
}
