// Package organize 按视频路径模板把刮削结果落盘：复制/移动视频、写 NFO、下载海报与剧照。
//
// 只保留「复制」和「移动」两种整理方式：硬链接与符号链接在宿主机 ↔ Docker 场景不可用。
package organize

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/fushuqian/amane-lite/internal/fetch"
	"github.com/fushuqian/amane-lite/internal/model"
	"github.com/fushuqian/amane-lite/internal/store"
)

// Options 是一次整理的参数。
type Options struct {
	Root          string
	Mode          string // copy / move
	VideoTemplate string
	WriteNFO      bool
	WriteImages   bool
}

// FileResult 记录单个源文件的目标路径。
type FileResult struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Error  string `json:"error,omitempty"`
}

// Result 是一次整理的产出与失败项。
type Result struct {
	Targets []string     `json:"targets"`
	Files   []FileResult `json:"files,omitempty"`
	Errors  []string     `json:"errors,omitempty"`
}

// Execute 整理一个条目的全部已索引文件。
func Execute(ctx context.Context, rec *store.MetaRecord, files []store.MediaFileRow, opt Options, client *fetch.Client) (Result, error) {
	var result Result
	if opt.Root == "" {
		return result, fmt.Errorf("未配置片库路径")
	}
	if len(files) == 0 {
		return result, fmt.Errorf("该条目没有已索引文件")
	}
	if err := os.MkdirAll(opt.Root, 0o755); err != nil {
		return result, err
	}
	if opt.Mode != "move" {
		opt.Mode = "copy"
	}
	imagesDone := map[string]bool{}
	for _, file := range files {
		item := FileResult{Source: file.Path}
		rel, err := RenderVideoPath(opt.VideoTemplate, rec, file)
		if err != nil {
			item.Error = err.Error()
			result.Files = append(result.Files, item)
			result.Errors = append(result.Errors, file.Path+": "+err.Error())
			continue
		}
		target := filepath.Join(opt.Root, filepath.FromSlash(rel))
		item.Target = target
		if err := placeVideo(file.Path, target, opt.Mode); err != nil {
			item.Error = err.Error()
			result.Files = append(result.Files, item)
			result.Errors = append(result.Errors, file.Path+": "+err.Error())
			continue
		}
		result.Targets = append(result.Targets, target)
		result.Files = append(result.Files, item)

		if opt.WriteNFO {
			ext := filepath.Ext(target)
			nfoPath := strings.TrimSuffix(target, ext) + ".nfo"
			if err := WriteNFO(rec, file, nfoPath, opt.WriteImages); err != nil {
				result.Errors = append(result.Errors, nfoPath+": "+err.Error())
			}
		}
		dir := filepath.Dir(target)
		if opt.WriteImages && client != nil && !imagesDone[dir] {
			imagesDone[dir] = true
			downloadImages(ctx, rec, dir, client, &result)
		}
	}
	if len(result.Files) > 0 && len(result.Targets) == 0 {
		return result, fmt.Errorf("全部文件整理失败")
	}
	return result, nil
}

// RenderVideoPath 按模板算出相对片库根的目标路径，并补齐源文件扩展名。
func RenderVideoPath(template string, rec *store.MetaRecord, file store.MediaFileRow) (string, error) {
	vals := buildValues(rec, file)
	raw, err := Render(template, vals)
	if err != nil {
		return "", err
	}
	rel, err := CleanRelPath(raw)
	if err != nil {
		return "", err
	}
	ext := strings.ToLower(filepath.Ext(file.Path))
	if ext != "" && filepath.Ext(rel) == "" {
		rel += ext
	}
	// 第二阶段：把 video_dir / video_name / video_relpath 注入后再渲染一次。
	if strings.Contains(template, "{video_") {
		dir, name := path.Split(rel)
		vals["video_dir"] = strings.TrimSuffix(dir, "/")
		vals["video_name"] = name
		vals["video_relpath"] = rel
		if second, err := Render(template, vals); err == nil {
			if rel2, err2 := CleanRelPath(second); err2 == nil {
				if ext != "" && filepath.Ext(rel2) == "" {
					rel2 += ext
				}
				rel = rel2
			}
		}
	}
	return rel, nil
}

func buildValues(rec *store.MetaRecord, file store.MediaFileRow) map[string]string {
	names := make([]string, 0, len(rec.Actors))
	for _, actor := range rec.Actors {
		if name := strings.TrimSpace(actor.Name); name != "" {
			names = append(names, truncateBytes(name, 200))
		}
	}
	first := ""
	if len(names) > 0 {
		first = names[0]
	}
	title := rec.Title
	if title == "" {
		title = rec.Number
	}
	year := ""
	if len(rec.Release) >= 4 {
		year = rec.Release[:4]
	}
	prefix, suffix := splitNumber(rec.Number)
	cd := ""
	if file.CD > 0 {
		cd = strconv.Itoa(file.CD)
	}
	sub := ""
	if file.HasSubtitle {
		sub = "C"
	}
	mosaic := string(file.Mosaic)
	if mosaic == "" && rec.ContentType == model.Censored {
		mosaic = "censored"
	}
	rawName := strings.TrimSuffix(filepath.Base(file.Path), filepath.Ext(file.Path))
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(file.Path), "."))
	return map[string]string{
		"number":        rec.Number,
		"prefix":        prefix,
		"suffix":        suffix,
		"title":         title,
		"actor":         first,
		"actors":        truncateBytes(strings.Join(names, ","), 200),
		"actress":       first,
		"actresses":     truncateBytes(strings.Join(names, ","), 200),
		"studio":        rec.Studio,
		"publisher":     rec.Publisher,
		"series":        rec.Series,
		"year":          year,
		"release":       rec.Release,
		"content_type":  string(rec.ContentType),
		"ext":           ext,
		"cd?":           cd,
		"sub?":          sub,
		"mosaic?":       mosaic,
		"def?":          file.Definition,
		"raw_name":      truncateBytes(rawName, 200),
		"raw_dir":       filepath.Base(filepath.Dir(file.Path)),
		"video_dir":     "",
		"video_name":    "",
		"video_relpath": "",
	}
}

func splitNumber(number string) (string, string) {
	if idx := strings.LastIndex(number, "-"); idx > 0 && idx < len(number)-1 {
		return number[:idx], number[idx+1:]
	}
	return number, ""
}

// CleanRelPath 清理相对路径：逐段去非法字符，拒绝绝对路径与 .. 段。
func CleanRelPath(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, `\`) {
		return "", errors.New("模板不能以 / 开头：片库路径由程序拼接")
	}
	parts := strings.Split(strings.ReplaceAll(raw, "\\", "/"), "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		if part == ".." {
			return "", errors.New("模板不能包含 .. 路径")
		}
		seg := Sanitize(part)
		if seg == "" {
			continue
		}
		out = append(out, seg)
	}
	if len(out) == 0 {
		return "", errors.New("模板渲染结果为空")
	}
	return strings.Join(out, "/"), nil
}

// Sanitize 清理路径分量：去掉非法字符，压缩空白，限制 200 字节。
func Sanitize(name string) string {
	name = strings.Map(func(r rune) rune {
		switch r {
		case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
			return ' '
		}
		if r < 0x20 {
			return ' '
		}
		return r
	}, name)
	name = strings.Join(strings.Fields(name), " ")
	name = strings.Trim(name, " .")
	return truncateBytes(name, 200)
}

func truncateBytes(value string, limit int) string {
	for len(value) > limit {
		value = value[:len(value)-1]
	}
	for len(value) > 0 && !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func placeVideo(src, dst, mode string) error {
	if sameFile(src, dst) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if srcInfo, err := os.Stat(src); err == nil {
		// 目标已存在且大小一致：视为已整理，移动模式下顺手清掉源文件。
		if dstInfo, derr := os.Stat(dst); derr == nil && dstInfo.Mode().IsRegular() && dstInfo.Size() == srcInfo.Size() {
			if mode == "move" {
				_ = os.Remove(src)
			}
			return nil
		}
	}
	if mode == "move" {
		if err := os.Rename(src, dst); err == nil {
			return nil
		}
		if err := copyFile(src, dst); err != nil {
			return err
		}
		return os.Remove(src)
	}
	return copyFile(src, dst)
}

func sameFile(src, dst string) bool {
	si, err := os.Stat(src)
	if err != nil {
		return false
	}
	di, err := os.Stat(dst)
	if err != nil {
		return false
	}
	return os.SameFile(si, di)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// WriteNFO 写出 Kodi 兼容的 movie.nfo。
func WriteNFO(rec *store.MetaRecord, file store.MediaFileRow, path string, withArt bool) error {
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<movie>\n")
	line := func(tag, value string) {
		if value == "" {
			return
		}
		b.WriteString("  <" + tag + ">" + escape(value) + "</" + tag + ">\n")
	}
	line("title", rec.Title)
	line("originaltitle", rec.Title)
	line("sorttitle", rec.Number)
	line("studio", rec.Studio)
	line("year", firstN(rec.Release, 4))
	line("premiered", rec.Release)
	if rec.Runtime > 0 {
		b.WriteString(fmt.Sprintf("  <runtime>%d</runtime>\n", rec.Runtime))
	}
	line("plot", rec.Plot)
	line("num", rec.Number)
	line("uniqueid", rec.Number)
	for _, actor := range rec.Actors {
		b.WriteString("  <actor><name>" + escape(actor.Name) + "</name><type>" +
			escape(string(actor.Gender)) + "</type></actor>\n")
	}
	for _, tag := range rec.Tags {
		line("genre", tag)
	}
	for _, director := range rec.Directors {
		line("director", director)
	}
	if withArt {
		b.WriteString("  <art><poster>poster.jpg</poster><fanart>fanart.jpg</fanart></art>\n")
	}
	b.WriteString("</movie>\n")
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func escape(value string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(value))
	return b.String()
}

func firstN(value string, n int) string {
	if len(value) <= n {
		return value
	}
	return value[:n]
}

func downloadImages(ctx context.Context, rec *store.MetaRecord, dir string, client *fetch.Client, result *Result) {
	type job struct{ url, name string }
	jobs := []job{}
	if len(rec.PosterURLs) > 0 {
		jobs = append(jobs, job{rec.PosterURLs[0], "poster.jpg"})
	}
	if len(rec.ThumbURLs) > 0 {
		jobs = append(jobs, job{rec.ThumbURLs[0], "thumb.jpg"})
	}
	fanart := ""
	if len(rec.Extrafanart) > 0 {
		fanart = rec.Extrafanart[0]
	} else if len(rec.ThumbURLs) > 0 {
		fanart = rec.ThumbURLs[0]
	}
	if fanart != "" {
		jobs = append(jobs, job{fanart, "fanart.jpg"})
	}
	for i, extra := range rec.Extrafanart {
		if i >= 6 {
			break
		}
		jobs = append(jobs, job{extra, filepath.Join("extrafanart", fmt.Sprintf("fanart-%02d.jpg", i+1))})
	}
	for _, item := range jobs {
		dest := filepath.Join(dir, item.name)
		// 已下载过就跳过：复制模式会保留源文件，避免每轮自动任务重复下载。
		if _, err := os.Stat(dest); err == nil {
			continue
		}
		opts := fetch.Options{}
		// 部分站点（javbus 等）校验图片的同源 Referer，否则返回 403。
		if u, uerr := url.Parse(item.url); uerr == nil && u.Host != "" {
			opts.Referer = u.Scheme + "://" + u.Host + "/"
		}
		if err := client.Download(ctx, item.url, dest, opts); err != nil {
			result.Errors = append(result.Errors, item.name+": "+err.Error())
		}
	}
}
