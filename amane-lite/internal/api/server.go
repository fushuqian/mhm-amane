// Package api 提供精简版的 HTTP 接口与模块前端静态文件。
package api

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fushuqian/amane-lite/internal/config"
	"github.com/fushuqian/amane-lite/internal/fetch"
	"github.com/fushuqian/amane-lite/internal/model"
	"github.com/fushuqian/amane-lite/internal/number"
	"github.com/fushuqian/amane-lite/internal/organize"
	"github.com/fushuqian/amane-lite/internal/sites"
	"github.com/fushuqian/amane-lite/internal/store"
	"github.com/fushuqian/amane-lite/internal/worker"
)

//go:embed all:web
var webFS embed.FS

// Version 是接口与前端展示的版本号。
const Version = "0.2.1"

// Server 组合存储、网络客户端与配置。
type Server struct {
	St     *store.Store
	Client *fetch.Client
	Cfg    *config.Manager
}

// Handler 返回带路由的 http.Handler。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/stats", s.stats)
	mux.HandleFunc("GET /api/metadata", s.listMetadata)
	mux.HandleFunc("GET /api/metadata/{number}", s.getMetadata)
	mux.HandleFunc("PATCH /api/metadata/{number}", s.patchMetadata)
	mux.HandleFunc("DELETE /api/metadata/{number}", s.deleteMetadata)
	mux.HandleFunc("GET /api/media", s.listMedia)
	mux.HandleFunc("GET /api/tasks", s.listTasks)
	mux.HandleFunc("POST /api/tasks", s.createTask)
	mux.HandleFunc("GET /api/config", s.getConfig)
	mux.HandleFunc("PATCH /api/config", s.patchConfig)
	mux.HandleFunc("GET /api/sites", s.listSites)
	mux.HandleFunc("POST /api/sites/check", s.checkSites)
	mux.HandleFunc("GET /api/fs", s.browseFS)
	mux.HandleFunc("POST /api/parse", s.parseText)
	mux.HandleFunc("POST /api/scan", s.scan)
	mux.HandleFunc("GET /api/image", s.imageProxy)

	static, err := fs.Sub(webFS, "web")
	if err == nil {
		fileServer := http.FileServer(http.FS(static))
		mux.Handle("GET /ui/", http.StripPrefix("/ui/", fileServer))
		mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				data, err := fs.ReadFile(static, strings.TrimPrefix(r.URL.Path, "/"))
				if err == nil {
					w.Header().Set("Content-Type", contentType(r.URL.Path))
					_, _ = w.Write(data)
					return
				}
			}
			data, _ := fs.ReadFile(static, "index.html")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(data)
		})
	}
	return withCORS(mux)
}

// TokenGuard 校验宿主注入的 X-Mica-Token；未设置 MICA_SIDECAR_TOKEN 时直接放行（本地开发）。
func TokenGuard(next http.Handler) http.Handler {
	token := os.Getenv("MICA_SIDECAR_TOKEN")
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Mica-Token") != token {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid sidecar token"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PATCH,DELETE,OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func contentType(path string) string {
	switch {
	case strings.HasSuffix(path, ".html"):
		return "text/html; charset=utf-8"
	case strings.HasSuffix(path, ".js"):
		return "text/javascript; charset=utf-8"
	case strings.HasSuffix(path, ".css"):
		return "text/css; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": Version})
}

func (s *Server) stats(w http.ResponseWriter, _ *http.Request) {
	stats, err := s.St.Stats()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"version": Version, "counts": stats})
}

func (s *Server) listMetadata(w http.ResponseWriter, r *http.Request) {
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, total, err := s.St.ListMetadata(offset, limit, r.URL.Query().Get("q"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

func (s *Server) getMetadata(w http.ResponseWriter, r *http.Request) {
	rec, err := s.St.GetMetadata(r.PathValue("number"))
	if err != nil {
		writeError(w, err)
		return
	}
	files, _ := s.St.ListMediaFiles(rec.Number)
	writeJSON(w, http.StatusOK, map[string]any{"metadata": rec, "files": files})
}

type patchRequest struct {
	Title      *string            `json:"title"`
	Plot       *string            `json:"plot"`
	Studio     *string            `json:"studio"`
	Publisher  *string            `json:"publisher"`
	Series     *string            `json:"series"`
	Release    *string            `json:"release"`
	Runtime    *int               `json:"runtime"`
	Actors     *[]model.FilmActor `json:"actors"`
	Tags       *[]string          `json:"tags"`
	Directors  *[]string          `json:"directors"`
	PosterURLs *[]string          `json:"poster_urls"`
	ThumbURLs  *[]string          `json:"thumb_urls"`
}

func (s *Server) patchMetadata(w http.ResponseWriter, r *http.Request) {
	rec, err := s.St.GetMetadata(r.PathValue("number"))
	if err != nil {
		writeError(w, err)
		return
	}
	var req patchRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.Title != nil {
		rec.Title = *req.Title
	}
	if req.Plot != nil {
		rec.Plot = *req.Plot
	}
	if req.Studio != nil {
		rec.Studio = *req.Studio
	}
	if req.Publisher != nil {
		rec.Publisher = *req.Publisher
	}
	if req.Series != nil {
		rec.Series = *req.Series
	}
	if req.Release != nil {
		rec.Release = *req.Release
	}
	if req.Runtime != nil {
		rec.Runtime = *req.Runtime
	}
	if req.Actors != nil {
		rec.Actors = *req.Actors
	}
	if req.Tags != nil {
		rec.Tags = *req.Tags
	}
	if req.Directors != nil {
		rec.Directors = *req.Directors
	}
	if req.PosterURLs != nil {
		rec.PosterURLs = *req.PosterURLs
	}
	if req.ThumbURLs != nil {
		rec.ThumbURLs = *req.ThumbURLs
	}
	agg := recordToAggregated(rec)
	if err := s.St.SaveMetadata(agg, rec.ContentType, rec.Raw); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"metadata": rec})
}

func recordToAggregated(rec *store.MetaRecord) model.Aggregated {
	return model.Aggregated{
		MediaMetadata: model.MediaMetadata{
			Number: rec.Number, Title: rec.Title, Plot: rec.Plot, Studio: rec.Studio,
			Publisher: rec.Publisher, Series: rec.Series, Release: rec.Release,
			Runtime: rec.Runtime, Score: rec.Score, Actors: rec.Actors, Tags: rec.Tags,
			Directors: rec.Directors, PosterURLs: rec.PosterURLs, ThumbURLs: rec.ThumbURLs,
			TrailerURLs: rec.TrailerURLs, Extrafanart: rec.Extrafanart,
			SourceURL: rec.SourceURL, ExternalID: rec.ExternalID,
		},
		FieldSources: rec.FieldSource,
		Sites:        rec.Sites,
	}
}

func (s *Server) deleteMetadata(w http.ResponseWriter, r *http.Request) {
	if err := s.St.DeleteMetadata(r.PathValue("number")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listMedia(w http.ResponseWriter, r *http.Request) {
	files, err := s.St.ListMediaFiles(r.URL.Query().Get("number"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": files})
}

func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	var (
		tasks []store.TaskRow
		err   error
	)
	if r.URL.Query().Get("active") == "1" {
		tasks, err = s.St.ListActiveTasks(limit)
	} else {
		tasks, err = s.St.ListTasks(limit)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": tasks})
}

type taskRequest struct {
	Kind    string   `json:"kind"`
	Numbers []string `json:"numbers,omitempty"`
	Path    string   `json:"path,omitempty"`
	Scrape  bool     `json:"scrape,omitempty"`
	Sites   []string `json:"sites,omitempty"`
}

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	var req taskRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	var id int64
	var err error
	switch req.Kind {
	case "scrape":
		if len(req.Numbers) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "numbers 不能为空"})
			return
		}
		id, err = s.St.CreateTask("scrape", worker.ScrapePayload{Numbers: req.Numbers, Sites: req.Sites})
	case "organize":
		if len(req.Numbers) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "numbers 不能为空"})
			return
		}
		id, err = s.St.CreateTask("organize", worker.OrganizePayload{Numbers: req.Numbers})
	case "scan":
		id, err = s.St.CreateTask("scan", worker.ScanPayload{Path: req.Path, Scrape: req.Scrape})
	case "sync":
		if active, aerr := s.St.HasActive("sync"); aerr != nil {
			writeError(w, aerr)
			return
		} else if active {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "已有自动任务在进行中"})
			return
		}
		id, err = s.St.CreateTask("sync", worker.SyncPayload{})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "kind 必须是 scrape / organize / scan / sync"})
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"id": id})
}

func (s *Server) getConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.Cfg.Get())
}

func (s *Server) patchConfig(w http.ResponseWriter, r *http.Request) {
	var next config.Config
	if err := readJSON(r, &next); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if len(next.Sites) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sites 不能为空"})
		return
	}
	// 先归一化：剥离模板里误贴的片库前缀、补默认值、修正非法整理方式。
	next.Normalize()
	roots := safeDirs()
	if !withinRoots(next.Library.Root, roots) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
			"error": fmt.Sprintf("片库路径必须在允许根内：%s", strings.Join(roots, ", ")),
		})
		return
	}
	for _, dir := range next.DownloadDirs {
		if !withinRoots(dir.Path, roots) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
				"error": fmt.Sprintf("下载目录必须在允许根内：%s", strings.Join(roots, ", ")),
			})
			return
		}
	}
	if err := organize.ValidateTemplate(next.Library.VideoTemplate); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	updated, err := s.Cfg.Update(func(cur *config.Config) error {
		*cur = next
		return nil
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if s.Client != nil {
		_ = s.Client.SetProxy(updated.EffectiveProxy())
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) listSites(w http.ResponseWriter, _ *http.Request) {
	type siteInfo struct {
		Name      string `json:"name"`
		Browser   string `json:"browser"`
		MultiLang bool   `json:"multi_language"`
		NeedsHash bool   `json:"uses_file_hash"`
	}
	out := []siteInfo{}
	for _, name := range sites.Names() {
		crawler, ok := sites.New(name, nil, sites.SiteConfig{})
		if !ok {
			continue
		}
		p := crawler.Profile()
		out = append(out, siteInfo{Name: name, Browser: string(p.Browser), MultiLang: p.MultiLang, NeedsHash: p.NeedsHash})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

// safeDirs 返回允许浏览的根目录（由宿主挂载决定）。
func safeDirs() []string {
	raw := os.Getenv("AMANE_SAFE_DIRS")
	if raw == "" {
		raw = "/media,/downloads"
	}
	out := []string{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, filepath.Clean(part))
		}
	}
	return out
}

func withinRoots(target string, roots []string) bool {
	clean := filepath.Clean(target)
	for _, root := range roots {
		root = filepath.Clean(root)
		if clean == root || strings.HasPrefix(clean, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// browseFS 目录浏览：只允许在允许根内活动，供设置页的路径选择器使用。
func (s *Server) browseFS(w http.ResponseWriter, r *http.Request) {
	roots := []string{}
	for _, root := range safeDirs() {
		if info, err := os.Stat(root); err == nil && info.IsDir() {
			roots = append(roots, root)
		}
	}
	type entry struct {
		Name  string `json:"name"`
		Path  string `json:"path"`
		IsDir bool   `json:"is_dir"`
	}
	target := r.URL.Query().Get("path")
	if target == "" {
		entries := make([]entry, 0, len(roots))
		for _, root := range roots {
			entries = append(entries, entry{Name: filepath.Base(root), Path: root, IsDir: true})
		}
		writeJSON(w, http.StatusOK, map[string]any{"path": "", "parent": "", "roots": roots, "entries": entries})
		return
	}
	if !withinRoots(target, roots) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "只能浏览挂载进来的片库/下载目录"})
		return
	}
	children, err := os.ReadDir(target)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "无法读取目录：" + err.Error()})
		return
	}
	entries := make([]entry, 0, len(children))
	for _, child := range children {
		if child.IsDir() {
			entries = append(entries, entry{Name: child.Name(), Path: filepath.Join(target, child.Name()), IsDir: true})
		}
	}
	parent := ""
	if target != filepath.Clean(target) || !isRoot(target, roots) {
		candidate := filepath.Dir(filepath.Clean(target))
		if withinRoots(candidate, roots) && !isRoot(target, roots) {
			parent = candidate
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": target, "parent": parent, "roots": roots, "entries": entries})
}

func isRoot(target string, roots []string) bool {
	clean := filepath.Clean(target)
	for _, root := range roots {
		if clean == filepath.Clean(root) {
			return true
		}
	}
	return false
}

// siteCheck 是站点连通性结果。
type siteCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// checkSites 并发探测各站点入口，返回 ok / blocked / unreachable / timeout。
func (s *Server) checkSites(w http.ResponseWriter, r *http.Request) {
	cfg := s.Cfg.Get()
	items := make([]siteCheck, len(cfg.Sites))
	var wg sync.WaitGroup
	for i, name := range cfg.Sites {
		crawler, ok := sites.New(name, s.Client, sites.SiteConfig{})
		if !ok {
			items[i] = siteCheck{Name: name, Status: "unreachable", Detail: "未注册的站点"}
			continue
		}
		base := crawler.Profile().BaseURL
		if setting, ok := cfg.SiteSettings[name]; ok && setting.BaseURL != "" {
			base = setting.BaseURL
		}
		wg.Add(1)
		go func(i int, name, base string) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()
			items[i] = probeSite(ctx, s.Client, name, base)
		}(i, name, base)
	}
	wg.Wait()
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func probeSite(ctx context.Context, client *fetch.Client, name, base string) siteCheck {
	out := siteCheck{Name: name}
	if base == "" {
		out.Status = "unreachable"
		out.Detail = "缺少站点入口"
		return out
	}
	_, err := client.GetHTML(ctx, base, fetch.Options{})
	switch {
	case err == nil:
		out.Status = "ok"
	case errors.Is(err, context.DeadlineExceeded) || isTimeout(err):
		out.Status = "timeout"
		out.Detail = "10 秒内未响应"
	case errors.Is(err, fetch.ErrBlocked):
		out.Status = "blocked"
		out.Detail = err.Error()
	default:
		out.Status = "unreachable"
		out.Detail = err.Error()
	}
	return out
}

func isTimeout(err error) bool {
	var netErr interface{ Timeout() bool }
	return errors.As(err, &netErr) && netErr.Timeout()
}

type parseRequest struct {
	Text string `json:"text"`
	Path string `json:"path,omitempty"`
}

func (s *Server) parseText(w http.ResponseWriter, r *http.Request) {
	var req parseRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	var info model.FileInfo
	if req.Path != "" {
		info = number.ParsePath(req.Path, nil)
	} else {
		info = number.ParseText(req.Text, nil)
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) scan(w http.ResponseWriter, r *http.Request) {
	var req worker.ScanPayload
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	id, err := s.St.CreateTask("scan", req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"id": id})
}

func (s *Server) imageProxy(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("url")
	if target == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "url 不能为空"})
		return
	}
	if u, err := url.Parse(target); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "只支持 http(s) 图片"})
		return
	} else {
		// 部分站点（javbus 等）校验图片的同源 Referer，否则返回 403。
		data, ctype, ferr := s.Client.GetBytes(r.Context(), target, fetch.Options{
			Referer: u.Scheme + "://" + u.Host + "/",
		})
		if ferr != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "拉取图片失败"})
			return
		}
		if ctype == "" {
			ctype = "image/jpeg"
		}
		w.Header().Set("Content-Type", ctype)
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = w.Write(data)
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, store.ErrMissing) {
		status = http.StatusNotFound
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func readJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("JSON 解析失败: %w", err)
	}
	return nil
}
