// Package worker 消费任务队列：自动扫描下载库、刮削、整理，另保留手动刮削/整理/扫描。
package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fushuqian/amane-lite/internal/aggregate"
	"github.com/fushuqian/amane-lite/internal/config"
	"github.com/fushuqian/amane-lite/internal/fetch"
	"github.com/fushuqian/amane-lite/internal/model"
	"github.com/fushuqian/amane-lite/internal/number"
	"github.com/fushuqian/amane-lite/internal/organize"
	"github.com/fushuqian/amane-lite/internal/sites"
	"github.com/fushuqian/amane-lite/internal/store"
)

// ScrapePayload 是刮削任务的入参。
type ScrapePayload struct {
	Numbers []string `json:"numbers"`
	Sites   []string `json:"sites,omitempty"`
}

// OrganizePayload 是整理任务的入参。
type OrganizePayload struct {
	Numbers []string `json:"numbers"`
}

// ScanPayload 是扫描任务的入参；Path 为空时扫描全部下载目录。
type ScanPayload struct {
	Path   string `json:"path"`
	Scrape bool   `json:"scrape"`
}

// SyncPayload 是自动任务的入参；目前不需要额外参数。
type SyncPayload struct{}

// Worker 串行消费任务；并发过高会集中触发站点反爬。
type Worker struct {
	st     *store.Store
	client *fetch.Client
	cfg    func() config.Config
}

// New 构造 worker。
func New(st *store.Store, client *fetch.Client, cfg func() config.Config) *Worker {
	return &Worker{st: st, client: client, cfg: cfg}
}

// Start 启动调度与消费循环，直到 ctx 取消。
func (w *Worker) Start(ctx context.Context) {
	go w.schedule(ctx)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			task, err := w.st.ClaimTask()
			if err != nil || task == nil {
				continue
			}
			result, err := w.handle(ctx, task)
			if err != nil {
				_ = w.st.FinishTask(task.ID, "failed", result, err.Error())
				continue
			}
			_ = w.st.FinishTask(task.ID, "done", result, "")
		}
	}
}

// schedule 每分钟检查一次，按配置入队自动 sync 任务。
func (w *Worker) schedule(ctx context.Context) {
	last := time.Now()
	if cfg := w.cfg(); cfg.Automation.RunOnStart && len(cfg.DownloadDirs) > 0 {
		if active, _ := w.st.HasActive("sync"); !active {
			_, _ = w.st.CreateTask("sync", SyncPayload{})
		}
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cfg := w.cfg()
			if !cfg.Automation.Enabled || len(cfg.DownloadDirs) == 0 {
				continue
			}
			interval := time.Duration(cfg.Automation.IntervalMinutes) * time.Minute
			if time.Since(last) < interval {
				continue
			}
			if active, err := w.st.HasActive("sync"); err != nil || active {
				continue
			}
			if _, err := w.st.CreateTask("sync", SyncPayload{}); err != nil {
				continue
			}
			last = time.Now()
		}
	}
}

func (w *Worker) handle(ctx context.Context, task *store.TaskRow) (string, error) {
	switch task.Kind {
	case "sync":
		return w.handleSync(ctx, task)
	case "scrape":
		return w.handleScrape(ctx, task)
	case "organize":
		return w.handleOrganize(ctx, task)
	case "scan":
		return w.handleScan(ctx, task)
	default:
		return "", fmt.Errorf("未知任务类型: %s", task.Kind)
	}
}

func (w *Worker) handleScrape(ctx context.Context, task *store.TaskRow) (string, error) {
	var payload ScrapePayload
	if err := json.Unmarshal([]byte(task.Payload), &payload); err != nil {
		return "", fmt.Errorf("payload 解析失败: %w", err)
	}
	order, cfgs := w.siteConfig(payload.Sites)

	type itemResult struct {
		Number   string              `json:"number"`
		Title    string              `json:"title,omitempty"`
		Found    bool                `json:"found"`
		Outcomes []aggregate.Outcome `json:"outcomes,omitempty"`
	}
	results := make([]itemResult, 0, len(payload.Numbers))
	for i, raw := range payload.Numbers {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		_ = w.st.UpdateProgress(task.ID, fmt.Sprintf("%d/%d %s", i+1, len(payload.Numbers), raw))
		meta, outcomes, found, err := w.scrapeOne(ctx, order, cfgs, raw)
		if err != nil {
			return "", err
		}
		item := itemResult{Number: raw, Outcomes: outcomes}
		if found {
			item.Number = meta.Number
			item.Title = meta.Title
			item.Found = true
		}
		results = append(results, item)
	}
	done := 0
	for _, item := range results {
		if item.Found {
			done++
		}
	}
	data, _ := json.Marshal(map[string]any{"items": results, "found": done})
	return string(data), nil
}

// scrapeOne 刮削并写库；返回聚合结果、站点明细与是否命中。
func (w *Worker) scrapeOne(ctx context.Context, order []string, cfgs map[string]sites.SiteConfig, raw string) (model.Aggregated, []aggregate.Outcome, bool, error) {
	info := number.ParseText(raw, nil)
	query := model.SearchQuery{Number: raw, ContentType: info.ContentType}
	if info.Number != "" {
		query.Number = info.Number
	}
	agg, outcomes, rawSites := aggregate.Run(ctx, order, w.client, cfgs, query)
	if agg.Number == "" || len(agg.Sites) == 0 {
		return agg, outcomes, false, nil
	}
	ctype := info.ContentType
	if ctype == "" {
		ctype = model.Western
	}
	rawJSON := make(map[string]json.RawMessage, len(rawSites))
	for site, data := range rawSites {
		rawJSON[site] = json.RawMessage(data)
	}
	if err := w.st.SaveMetadata(agg, ctype, rawJSON); err != nil {
		return agg, outcomes, false, fmt.Errorf("写入元数据失败: %w", err)
	}
	return agg, outcomes, true, nil
}

func (w *Worker) siteConfig(override []string) ([]string, map[string]sites.SiteConfig) {
	cfg := w.cfg()
	order := cfg.Sites
	if len(override) > 0 {
		order = override
	}
	cfgs := map[string]sites.SiteConfig{}
	for name, s := range cfg.SiteSettings {
		cfgs[name] = sites.SiteConfig{BaseURL: s.BaseURL, Cookies: s.Cookies}
	}
	return order, cfgs
}

func (w *Worker) handleOrganize(ctx context.Context, task *store.TaskRow) (string, error) {
	var payload OrganizePayload
	if err := json.Unmarshal([]byte(task.Payload), &payload); err != nil {
		return "", fmt.Errorf("payload 解析失败: %w", err)
	}
	cfg := w.cfg()
	type itemResult struct {
		Number string            `json:"number"`
		Result []organize.Result `json:"result"`
		Error  string            `json:"error,omitempty"`
	}
	items := make([]itemResult, 0, len(payload.Numbers))
	for _, raw := range payload.Numbers {
		rec, err := w.st.GetMetadata(raw)
		if err != nil {
			items = append(items, itemResult{Number: raw, Error: err.Error()})
			continue
		}
		files, err := w.st.ListMediaFiles(rec.Number)
		if err != nil {
			items = append(items, itemResult{Number: raw, Error: err.Error()})
			continue
		}
		item := itemResult{Number: rec.Number}
		for _, group := range groupByMode(cfg, files) {
			result, err := organize.Execute(ctx, rec, group.files, w.options(cfg, group.mode), w.client)
			item.Result = append(item.Result, result)
			if err != nil {
				item.Error = err.Error()
			}
			w.syncMovedPaths(result)
		}
		items = append(items, item)
	}
	data, _ := json.Marshal(map[string]any{"items": items})
	return string(data), nil
}

var videoExts = map[string]struct{}{
	".mp4": {}, ".mkv": {}, ".avi": {}, ".wmv": {}, ".ts": {}, ".m2ts": {}, ".mov": {},
	".rmvb": {}, ".flv": {}, ".webm": {}, ".mpg": {}, ".mpeg": {}, ".iso": {},
}

type scannedFile struct {
	Path string
	Info model.FileInfo
	Size int64
}

func (w *Worker) handleScan(_ context.Context, task *store.TaskRow) (string, error) {
	var payload ScanPayload
	if err := json.Unmarshal([]byte(task.Payload), &payload); err != nil {
		return "", fmt.Errorf("payload 解析失败: %w", err)
	}
	roots := w.scanRoots(payload.Path)
	if len(roots) == 0 {
		return "", errors.New("没有可扫描的下载目录")
	}
	files, scanned := scanDirs(roots, func(n int) {
		_ = w.st.UpdateProgress(task.ID, fmt.Sprintf("扫描：已发现 %d 个视频", n))
	})
	var registered int
	numbers := map[string]struct{}{}
	for _, file := range files {
		if err := w.st.RegisterMediaFile(file.Path, file.Info, file.Size); err != nil {
			continue
		}
		registered++
		numbers[file.Info.Number] = struct{}{}
	}
	var result struct {
		Scanned    int      `json:"scanned"`
		Registered int      `json:"registered"`
		Numbers    []string `json:"numbers"`
	}
	result.Scanned = scanned
	result.Registered = registered
	for n := range numbers {
		result.Numbers = append(result.Numbers, n)
	}
	sort.Strings(result.Numbers)
	if payload.Scrape && len(result.Numbers) > 0 {
		if _, err := w.st.CreateTask("scrape", ScrapePayload{Numbers: result.Numbers}); err != nil {
			return "", err
		}
	}
	data, _ := json.Marshal(result)
	return string(data), nil
}

// handleSync 是自动链路：扫描下载库 → 新番号刮削 → 按来源目录整理。
func (w *Worker) handleSync(ctx context.Context, task *store.TaskRow) (string, error) {
	cfg := w.cfg()
	if len(cfg.DownloadDirs) == 0 {
		return "", errors.New("未配置下载目录，请先在设置里添加")
	}
	roots := w.scanRoots("")
	if len(roots) == 0 {
		return "", errors.New("下载目录都不可用")
	}
	files, _ := scanDirs(roots, func(n int) {
		if n%10 == 0 {
			_ = w.st.UpdateProgress(task.ID, fmt.Sprintf("扫描：已发现 %d 个视频", n))
		}
	})

	numbers := map[string]struct{}{}
	var registered int
	for _, file := range files {
		if err := w.st.RegisterMediaFile(file.Path, file.Info, file.Size); err != nil {
			continue
		}
		registered++
		numbers[file.Info.Number] = struct{}{}
	}

	order, cfgs := w.siteConfig(nil)
	toScrape := make([]string, 0, len(numbers))
	for n := range numbers {
		if _, err := w.st.GetMetadata(n); errors.Is(err, store.ErrMissing) {
			toScrape = append(toScrape, n)
		}
	}
	sort.Strings(toScrape)
	var scraped int
	for i, n := range toScrape {
		_ = w.st.UpdateProgress(task.ID, fmt.Sprintf("刮削 %d/%d %s", i+1, len(toScrape), n))
		if _, _, found, err := w.scrapeOne(ctx, order, cfgs, n); err == nil && found {
			scraped++
		}
	}

	// 按（番号、整理方式）分组，一个番号一次整理，避免重复下载图片。
	type groupKey struct {
		Number string
		Mode   string
	}
	groups := map[groupKey][]string{}
	keyOrder := []groupKey{}
	for _, file := range files {
		key := groupKey{Number: file.Info.Number, Mode: modeForPath(cfg, file.Path)}
		if _, ok := groups[key]; !ok {
			keyOrder = append(keyOrder, key)
		}
		groups[key] = append(groups[key], file.Path)
	}

	var organized int
	errs := []string{}
	for i, key := range keyOrder {
		_ = w.st.UpdateProgress(task.ID, fmt.Sprintf("整理 %d/%d %s", i+1, len(keyOrder), key.Number))
		rec, err := w.st.GetMetadata(key.Number)
		if err != nil {
			errs = append(errs, key.Number+": "+err.Error())
			continue
		}
		wanted := map[string]struct{}{}
		for _, p := range groups[key] {
			wanted[p] = struct{}{}
		}
		all, err := w.st.ListMediaFiles(key.Number)
		if err != nil {
			errs = append(errs, key.Number+": "+err.Error())
			continue
		}
		subset := make([]store.MediaFileRow, 0, len(wanted))
		for _, f := range all {
			if _, ok := wanted[f.Path]; ok {
				subset = append(subset, f)
			}
		}
		if len(subset) == 0 {
			continue
		}
		result, err := organize.Execute(ctx, rec, subset, w.options(cfg, key.Mode), w.client)
		organized += len(result.Targets)
		errs = append(errs, result.Errors...)
		if err != nil {
			errs = append(errs, key.Number+": "+err.Error())
		}
		w.syncMovedPaths(result)
	}

	var result struct {
		Scanned    int      `json:"scanned"`
		Registered int      `json:"registered"`
		Scraped    int      `json:"scraped"`
		Organized  int      `json:"organized"`
		Errors     []string `json:"errors,omitempty"`
	}
	result.Scanned = len(files)
	result.Registered = registered
	result.Scraped = scraped
	result.Organized = organized
	if len(errs) > 50 {
		errs = errs[:50]
	}
	result.Errors = errs
	data, _ := json.Marshal(result)
	return string(data), nil
}

// scanRoots 返回存在的扫描根：指定 path 优先，否则全部下载目录。
func (w *Worker) scanRoots(explicit string) []string {
	if explicit != "" {
		if _, err := os.Stat(explicit); err == nil {
			return []string{explicit}
		}
		return nil
	}
	roots := []string{}
	for _, dir := range w.cfg().DownloadDirs {
		if _, err := os.Stat(dir.Path); err == nil {
			roots = append(roots, dir.Path)
		}
	}
	return roots
}

// scanDirs 遍历目录收集视频文件；progress 按已发现数量回调。
func scanDirs(roots []string, progress func(int)) ([]scannedFile, int) {
	out := []scannedFile{}
	scanned := 0
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if strings.HasPrefix(d.Name(), ".") && path != root {
					return filepath.SkipDir
				}
				return nil
			}
			if _, ok := videoExts[strings.ToLower(filepath.Ext(path))]; !ok {
				return nil
			}
			info := number.ParsePath(path, nil)
			if info.Number == "" {
				return nil
			}
			scanned++
			if progress != nil {
				progress(scanned)
			}
			var size int64
			if stat, err := d.Info(); err == nil {
				size = stat.Size()
			}
			out = append(out, scannedFile{Path: path, Info: info, Size: size})
			return nil
		})
	}
	return out, scanned
}

// modeForPath 用最长前缀匹配找到文件所属下载目录的整理方式。
func modeForPath(cfg config.Config, path string) string {
	best := ""
	mode := config.ModeCopy
	for _, dir := range cfg.DownloadDirs {
		root := strings.TrimSuffix(dir.Path, "/")
		if path == root || strings.HasPrefix(path, root+"/") {
			if len(root) > len(best) {
				best = root
				mode = dir.Mode
			}
		}
	}
	return mode
}

func (w *Worker) options(cfg config.Config, mode string) organize.Options {
	return organize.Options{
		Root:          cfg.Library.Root,
		Mode:          mode,
		VideoTemplate: cfg.Library.VideoTemplate,
		WriteNFO:      cfg.Library.WriteNFO,
		WriteImages:   cfg.Library.WriteImages,
	}
}

// syncMovedPaths 把移动后的文件路径写回索引，避免下次扫描重复整理。
func (w *Worker) syncMovedPaths(result organize.Result) {
	for _, item := range result.Files {
		if item.Error != "" || item.Target == "" {
			continue
		}
		if _, err := os.Stat(item.Source); errors.Is(err, os.ErrNotExist) {
			_ = w.st.UpdateMediaFilePath(item.Source, item.Target)
		}
	}
}

type modeGroup struct {
	mode  string
	files []store.MediaFileRow
}

func groupByMode(cfg config.Config, files []store.MediaFileRow) []modeGroup {
	index := map[string]int{}
	out := []modeGroup{}
	for _, file := range files {
		mode := modeForPath(cfg, file.Path)
		i, ok := index[mode]
		if !ok {
			i = len(out)
			index[mode] = i
			out = append(out, modeGroup{mode: mode})
		}
		out[i].files = append(out[i].files, file)
	}
	return out
}
