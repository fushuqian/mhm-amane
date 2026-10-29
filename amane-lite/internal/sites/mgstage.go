// mgstage 爬虫：移植自 Amane 的 src/amane/crawlers/sites/mgstage.py。
package sites

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"

	"github.com/fushuqian/amane-lite/internal/fetch"
	"github.com/fushuqian/amane-lite/internal/model"
	"github.com/fushuqian/amane-lite/internal/number"
)

const (
	mgCoverPrefix  = "pb_e_"
	mgPosterPrefix = "pf_e_"
	mgAmateurTag   = "素人"
)

var reMGDetail = regexp.MustCompile(`/product/product_detail/([^/]+)/`)

func init() {
	Register("mgstage", func(client *fetch.Client, cfg SiteConfig) Crawler {
		base := &baseCrawler{client: client}
		base.profile = Profile{
			Name:    "mgstage",
			BaseURL: "https://www.mgstage.com",
			Cookies: map[string]string{"adc": "1"},
			Browser: BrowserAuto,
		}
		base.baseURL = base.resolveBase(base.profile, cfg)
		base.cookies = base.mergeCookies(base.profile, cfg)
		return &mgstage{baseCrawler: base}
	})
}

type mgstage struct{ *baseCrawler }

func (s *mgstage) Profile() Profile { return s.profile }

func (s *mgstage) Fetch(ctx context.Context, q model.SearchQuery) (*model.MediaMetadata, error) {
	for _, variant := range number.NumberVariants(q.Number) {
		url, err := s.searchOne(ctx, variant, q.Number)
		if err != nil {
			continue
		}
		if url != "" {
			return s.scrape(ctx, url)
		}
	}
	return nil, fetch.ErrNotFound
}

func (s *mgstage) searchOne(ctx context.Context, variant, queryNumber string) (string, error) {
	direct := fmt.Sprintf("%s/product/product_detail/%s/", s.baseURL, variant)
	if resp, err := s.client.GetHTML(ctx, direct, s.options("")); err == nil {
		if Text(Doc(resp.Body), `//*[@id="center_column"]/div[1]/h1`) != "" {
			return direct, nil
		}
	}

	searchURL := fmt.Sprintf("%s/search/cSearch.php?search_word=%s", s.baseURL, variant)
	resp, err := s.client.GetHTML(ctx, searchURL, s.options(""))
	if err != nil {
		return "", err
	}
	doc := Doc(resp.Body)
	var urls, found []string
	seen := map[string]struct{}{}
	for _, node := range xpathFind(doc, `//a[contains(@href, "/product/product_detail/")]`) {
		href := absHref(s.baseURL, node)
		if href == "" {
			continue
		}
		m := reMGDetail.FindStringSubmatch(href)
		if m == nil {
			continue
		}
		if _, ok := seen[href]; ok {
			continue
		}
		seen[href] = struct{}{}
		urls = append(urls, href)
		found = append(found, m[1])
	}
	index := number.SelectNumberIndex(found, queryNumber)
	if index < 0 {
		return "", fetch.ErrNotFound
	}
	return urls[index], nil
}

func (s *mgstage) scrape(ctx context.Context, url string) (*model.MediaMetadata, error) {
	resp, err := s.client.GetHTML(ctx, url, s.options(url))
	if err != nil {
		return nil, err
	}
	doc := Doc(resp.Body)

	title := Text(doc, `//*[@id="center_column"]/div[1]/h1`)
	if title == "" {
		return nil, fetch.ErrNotFound
	}

	numberText := ""
	if m := regexp.MustCompile(`/product_detail/([^/]+)`).FindStringSubmatch(url); m != nil {
		numberText = m[1]
	}

	tags := Texts(doc, `//th[contains(text(),"ジャンル")]/following-sibling::td/a`)
	amateur := false
	for _, tag := range tags {
		if strings.Contains(tag, mgAmateurTag) {
			amateur = true
			break
		}
	}
	var actors []model.FilmActor
	if !amateur {
		actors = Actors(Texts(doc, `//th[contains(text(),"出演")]/following-sibling::td/a`))
	}

	runtime := 0
	if raw := Text(doc, `//th[contains(text(),"収録時間")]/following-sibling::td/text()`); raw != "" {
		if m := regexp.MustCompile(`(\d+)`).FindStringSubmatch(raw); m != nil {
			runtime, _ = strconv.Atoi(m[1])
		}
	}

	cover := Attr(doc, `//a[@id="EnlargeImage"]`, "href")
	if cover == "" {
		cover = Attr(doc, `//img[@class="enlarge_image"]`, "src")
	}
	cover = AbsURL(url, cover)
	poster := mgPosterURL(cover)

	extrafanart := []string{}
	for _, node := range xpathFind(doc, `//dl[@id="sample-photo"]//li/a`) {
		if href := attrOf(node, "href"); href != "" {
			extrafanart = append(extrafanart, AbsURL(url, href))
		}
	}

	plot := strings.Join(Texts(doc, `//dl[@id="introduction"]/dd/p[not(contains(@class, "more"))]//text()`), "\n")

	return &model.MediaMetadata{
		Number:      numberText,
		Title:       title,
		Actors:      actors,
		Studio:      Text(doc, `//th[contains(text(),"メーカー")]/following-sibling::td/a`),
		Publisher:   Text(doc, `//th[contains(text(),"レーベル")]/following-sibling::td/a`),
		Series:      Text(doc, `//th[contains(text(),"シリーズ")]/following-sibling::td/a`),
		Release:     Text(doc, `//th[contains(text(),"配信開始日")]/following-sibling::td/text()`),
		Runtime:     runtime,
		Tags:        tags,
		Plot:        plot,
		PosterURLs:  nonEmpty(poster),
		ThumbURLs:   nonEmpty(cover),
		Extrafanart: extrafanart,
		SourceURL:   url,
		ExternalID:  url,
	}, nil
}

func mgPosterURL(cover string) string {
	directory, filename := "", ""
	if i := strings.LastIndex(cover, "/"); i >= 0 {
		directory, filename = cover[:i], cover[i+1:]
	}
	if directory == "" || !strings.HasPrefix(filename, mgCoverPrefix) {
		return ""
	}
	return directory + "/" + mgPosterPrefix + strings.TrimPrefix(filename, mgCoverPrefix)
}

func absHref(base string, node *html.Node) string {
	return AbsURL(base, attrOf(node, "href"))
}
