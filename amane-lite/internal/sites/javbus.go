// javbus 爬虫：移植自 Amane 的 src/amane/crawlers/sites/javbus.py。
package sites

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/fushuqian/amane-lite/internal/fetch"
	"github.com/fushuqian/amane-lite/internal/model"
	"github.com/fushuqian/amane-lite/internal/number"
)

func init() {
	Register("javbus", func(client *fetch.Client, cfg SiteConfig) Crawler {
		base := &baseCrawler{client: client}
		base.profile = Profile{
			Name:    "javbus",
			BaseURL: "https://www.javbus.com",
			Cookies: map[string]string{"dv": "1"},
			Headers: map[string]string{
				"Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8,en-GB;q=0.7,en-US;q=0.6",
			},
			Referer: true,
			Browser: BrowserAuto,
		}
		base.baseURL = base.resolveBase(base.profile, cfg)
		base.cookies = base.mergeCookies(base.profile, cfg)
		return &javbus{baseCrawler: base}
	})
}

type javbus struct{ *baseCrawler }

func (s *javbus) Profile() Profile { return s.profile }

func (s *javbus) Fetch(ctx context.Context, q model.SearchQuery) (*model.MediaMetadata, error) {
	url, err := s.search(ctx, q)
	if err != nil {
		return nil, err
	}
	return s.scrape(ctx, url)
}

// search 先按 numberVariants 尝试直达详情页，未命中再回退站内检索。
func (s *javbus) search(ctx context.Context, q model.SearchQuery) (string, error) {
	for _, variant := range number.NumberVariants(q.Number) {
		url, err := s.searchOne(ctx, variant, q.Number)
		if err != nil && !errors.Is(err, fetch.ErrNotFound) {
			continue // 单次请求失败不终止整轮尝试
		}
		if url != "" {
			return url, nil
		}
	}
	return "", fetch.ErrNotFound
}

func (s *javbus) searchOne(ctx context.Context, variant, queryNumber string) (string, error) {
	direct := fmt.Sprintf("%s/%s", s.baseURL, variant)
	resp, err := s.client.GetHTML(ctx, direct, s.options(""))
	if err == nil && resp != nil && strings.Contains(resp.Body, "movie-box") {
		doc := Doc(resp.Body)
		if Text(doc, "//h3") != "" {
			return direct, nil
		}
	}

	searchURL := fmt.Sprintf("%s/search/%s&type=&parent=ce", s.baseURL, variant)
	resp, err = s.client.GetHTML(ctx, searchURL, s.options(""))
	if err != nil {
		return "", err
	}
	doc := Doc(resp.Body)
	var urls, found []string
	for _, node := range xpathFind(doc, "//a[@class='movie-box']") {
		href := attrOf(node, "href")
		if href == "" {
			continue
		}
		nums := xpathFind(node, "div[@class='photo-info']//date[1]/text()")
		if len(nums) == 0 {
			continue
		}
		urls = append(urls, AbsURL(s.baseURL, href))
		found = append(found, nodeTextOf(nums[0]))
	}
	index := number.SelectNumberIndex(found, queryNumber)
	if index < 0 {
		return "", fetch.ErrNotFound
	}
	return urls[index], nil
}

func (s *javbus) scrape(ctx context.Context, url string) (*model.MediaMetadata, error) {
	resp, err := s.client.GetHTML(ctx, url, s.options(url))
	if err != nil {
		return nil, err
	}
	doc := Doc(resp.Body)

	title := Text(doc, "//h3")
	if title == "" {
		return nil, fetch.ErrNotFound
	}
	numberText := Text(doc, `//span[@class="header"][contains(text(), "識別碼:")]/following-sibling::span`)
	if numberText == "" {
		return nil, fetch.ErrNotFound
	}

	cover := Attr(doc, `//a[@class="bigImage"]`, "href")
	thumb := AbsURL(url, cover)
	poster := ""
	if cover != "" {
		poster = strings.NewReplacer("/cover/", "/thumb/", "_b.jpg", ".jpg").Replace(cover)
		poster = AbsURL(url, poster)
	}

	runtime := 0
	if raw := Text(doc, `//span[@class="header"][contains(text(), "長度:")]/parent::p/text()`); raw != "" {
		if m := regexp.MustCompile(`(\d+)`).FindStringSubmatch(raw); m != nil {
			runtime, _ = strconv.Atoi(m[1])
		}
	}

	director := Text(doc, `//a[contains(@href, "/director/")]`)
	var directors []string
	if director != "" {
		directors = []string{director}
	}

	extrafanart := []string{}
	for _, node := range xpathFind(doc, "//div[@id='sample-waterfall']/a") {
		if href := attrOf(node, "href"); href != "" {
			extrafanart = append(extrafanart, AbsURL(url, href))
		}
	}

	return &model.MediaMetadata{
		Number:      numberText,
		Title:       title,
		Actors:      Actors(Texts(doc, `//div[@class="star-name"]/a`)),
		Studio:      Text(doc, `//a[contains(@href, "/studio/")]`),
		Publisher:   Text(doc, `//a[contains(@href, "/label/")]`),
		Release:     Text(doc, `//span[@class="header"][contains(text(), "發行日期:")]/parent::p/text()`),
		Runtime:     runtime,
		Tags:        Texts(doc, `//span[@class="genre"]/label/a[contains(@href, "/genre/")]`),
		Series:      Text(doc, `//a[contains(@href, "/series/")]`),
		Directors:   directors,
		PosterURLs:  nonEmpty(poster),
		ThumbURLs:   nonEmpty(thumb),
		Extrafanart: extrafanart,
		SourceURL:   url,
		ExternalID:  url,
	}, nil
}

func nonEmpty(value string) []string {
	if value == "" {
		return nil
	}
	return []string{value}
}
