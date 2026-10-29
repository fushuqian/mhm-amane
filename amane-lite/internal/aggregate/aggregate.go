// Package aggregate 把多个站点的结果按站点顺序聚合：标量取首个非空，列表整体取首个非空，图片按站点拼接。
package aggregate

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/fushuqian/amane-lite/internal/fetch"
	"github.com/fushuqian/amane-lite/internal/model"
	"github.com/fushuqian/amane-lite/internal/sites"
)

// Outcome 记录单个站点的执行结果，供任务日志与界面展示。
type Outcome struct {
	Site     string        `json:"site"`
	Found    bool          `json:"found"`
	Error    string        `json:"error,omitempty"`
	Duration time.Duration `json:"-"`
}

// Run 按 order 指定的站点顺序聚合；某站失败不阻断其它站。
func Run(ctx context.Context, order []string, client *fetch.Client, cfgs map[string]sites.SiteConfig, q model.SearchQuery) (model.Aggregated, []Outcome, map[string][]byte) {
	type result struct {
		site string
		meta *model.MediaMetadata
		out  Outcome
	}
	results := make([]result, len(order))
	var wg sync.WaitGroup
	for i, name := range order {
		crawler, ok := sites.New(name, client, cfgs[name])
		if !ok {
			results[i] = result{site: name, out: Outcome{Site: name, Error: "unknown site"}}
			continue
		}
		wg.Add(1)
		go func(i int, name string, crawler sites.Crawler) {
			defer wg.Done()
			started := time.Now()
			meta, err := crawler.Fetch(ctx, q)
			out := Outcome{Site: name, Duration: time.Since(started)}
			switch {
			case err != nil:
				out.Error = err.Error()
			case meta == nil:
				out.Error = "no result"
			default:
				out.Found = true
			}
			results[i] = result{site: name, meta: meta, out: out}
		}(i, name, crawler)
	}
	wg.Wait()

	agg := model.Aggregated{FieldSources: map[string]string{}}
	outcomes := make([]Outcome, 0, len(results))
	raw := map[string][]byte{}
	var posterSeen, thumbSeen, trailerSeen, artSeen = map[string]struct{}{}, map[string]struct{}{}, map[string]struct{}{}, map[string]struct{}{}
	for _, item := range results {
		outcomes = append(outcomes, item.out)
		meta := item.meta
		if meta == nil {
			continue
		}
		if data, err := json.Marshal(meta); err == nil {
			raw[item.site] = data
		}
		agg.Sites = append(agg.Sites, item.site)
		scalar := func(dst *string, value string, field string) {
			if *dst == "" && value != "" {
				*dst = value
				agg.FieldSources[field] = item.site
			}
		}
		scalar(&agg.Number, meta.Number, "number")
		scalar(&agg.Title, meta.Title, "title")
		scalar(&agg.Studio, meta.Studio, "studio")
		scalar(&agg.Publisher, meta.Publisher, "publisher")
		scalar(&agg.Series, meta.Series, "series")
		scalar(&agg.Release, meta.Release, "release")
		scalar(&agg.Plot, meta.Plot, "plot")
		scalar(&agg.SourceURL, meta.SourceURL, "source_url")
		scalar(&agg.ExternalID, meta.ExternalID, "external_id")
		if agg.Runtime == 0 && meta.Runtime > 0 {
			agg.Runtime = meta.Runtime
			agg.FieldSources["runtime"] = item.site
		}
		if agg.Score == 0 && meta.Score > 0 {
			agg.Score = meta.Score
			agg.FieldSources["score"] = item.site
		}
		if len(agg.Actors) == 0 && len(meta.Actors) > 0 {
			agg.Actors = meta.Actors
			agg.FieldSources["actors"] = item.site
		}
		if len(agg.Tags) == 0 && len(meta.Tags) > 0 {
			agg.Tags = meta.Tags
			agg.FieldSources["tags"] = item.site
		}
		if len(agg.Directors) == 0 && len(meta.Directors) > 0 {
			agg.Directors = meta.Directors
			agg.FieldSources["directors"] = item.site
		}
		agg.PosterURLs = appendUnique(agg.PosterURLs, posterSeen, meta.PosterURLs)
		agg.ThumbURLs = appendUnique(agg.ThumbURLs, thumbSeen, meta.ThumbURLs)
		agg.TrailerURLs = appendUnique(agg.TrailerURLs, trailerSeen, meta.TrailerURLs)
		agg.Extrafanart = appendUnique(agg.Extrafanart, artSeen, meta.Extrafanart)
	}
	if agg.Number == "" {
		agg.Number = q.Number
	}
	if len(agg.PosterURLs) == 0 {
		agg.PosterURLs = append([]string{}, agg.ThumbURLs...)
	}
	sort.SliceStable(outcomes, func(i, j int) bool { return outcomes[i].Site < outcomes[j].Site })
	return agg, outcomes, raw
}

func appendUnique(dst []string, seen map[string]struct{}, values []string) []string {
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		dst = append(dst, value)
	}
	return dst
}
