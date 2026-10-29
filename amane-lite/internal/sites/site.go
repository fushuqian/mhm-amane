// Package sites 放站点爬虫：站点声明、注册表与共用解析工具。
package sites

import (
	"context"
	"net/url"
	"strings"

	"github.com/antchfx/htmlquery"
	"golang.org/x/net/html"

	"github.com/fushuqian/amane-lite/internal/fetch"
	"github.com/fushuqian/amane-lite/internal/model"
)

// BrowserMode 是站点的浏览器使用策略；Go 版默认只直连，命中挑战后可由调用方重试浏览器。
type BrowserMode string

const (
	BrowserOff    BrowserMode = "off"
	BrowserAuto   BrowserMode = "auto"
	BrowserAlways BrowserMode = "always"
)

// Profile 是站点的静态声明：入口、cookie、请求头与反爬策略。
type Profile struct {
	Name      string
	BaseURL   string
	Cookies   map[string]string
	Headers   map[string]string
	Referer   bool // 同源 Referer（部分站点校验静态资源来源）
	Browser   BrowserMode
	MultiLang bool
	NeedsHash bool
}

// SiteConfig 是运行期覆盖项（用户在设置里改域名/cookie）。
type SiteConfig struct {
	BaseURL string
	Cookies map[string]string
}

// Crawler 是一个站点爬虫。实例无状态，可并发复用。
type Crawler interface {
	Profile() Profile
	// Fetch 完成「检索 → 详情」两步；未命中返回 fetch.ErrNotFound。
	Fetch(ctx context.Context, q model.SearchQuery) (*model.MediaMetadata, error)
}

// Factory 按运行期配置构造爬虫。
type Factory func(client *fetch.Client, cfg SiteConfig) Crawler

var registry = map[string]Factory{}

// Register 注册站点；重复注册会覆盖，便于测试替换。
func Register(name string, factory Factory) { registry[name] = factory }

// Names 返回已注册站点名（按注册顺序）。
func Names() []string {
	out := make([]string, 0, len(registry))
	for name := range registry {
		out = append(out, name)
	}
	return out
}

// New 构造指定站点；未注册返回 false。
func New(name string, client *fetch.Client, cfg SiteConfig) (Crawler, bool) {
	factory, ok := registry[name]
	if !ok {
		return nil, false
	}
	return factory(client, cfg), true
}

func (s *baseCrawler) resolveBase(p Profile, cfg SiteConfig) string {
	if cfg.BaseURL != "" {
		return strings.TrimSuffix(cfg.BaseURL, "/")
	}
	return strings.TrimSuffix(p.BaseURL, "/")
}

func (s *baseCrawler) mergeCookies(p Profile, cfg SiteConfig) map[string]string {
	out := make(map[string]string, len(p.Cookies)+len(cfg.Cookies))
	for k, v := range p.Cookies {
		out[k] = v
	}
	for k, v := range cfg.Cookies {
		out[k] = v
	}
	return out
}

func (s *baseCrawler) options(referer string) fetch.Options {
	opts := fetch.Options{Headers: s.profile.Headers, Cookies: s.cookies}
	if s.profile.Referer {
		if referer == "" {
			referer = s.baseURL + "/"
		}
		opts.Referer = referer
	}
	return opts
}

type baseCrawler struct {
	client  *fetch.Client
	baseURL string
	cookies map[string]string
	profile Profile
}

// Doc 解析 HTML 文档。
func Doc(body string) *html.Node {
	doc, err := htmlquery.Parse(strings.NewReader(body))
	if err != nil {
		return nil
	}
	return doc
}

// Text 依次尝试多个 XPath，返回第一个非空文本。
func Text(doc *html.Node, exprs ...string) string {
	for _, expr := range exprs {
		nodes := htmlquery.Find(doc, expr)
		for _, node := range nodes {
			if value := nodeText(node); value != "" {
				return value
			}
		}
	}
	return ""
}

// Texts 返回第一个有结果的 XPath 的全部非空文本。
func Texts(doc *html.Node, exprs ...string) []string {
	for _, expr := range exprs {
		values := []string{}
		for _, node := range htmlquery.Find(doc, expr) {
			if value := nodeText(node); value != "" {
				values = append(values, value)
			}
		}
		if len(values) > 0 {
			return values
		}
	}
	return nil
}

// Attr 返回第一个匹配节点的属性值。
func Attr(doc *html.Node, expr, attr string) string {
	for _, node := range htmlquery.Find(doc, expr) {
		if value := strings.TrimSpace(htmlquery.SelectAttr(node, attr)); value != "" {
			return value
		}
	}
	return ""
}

// AbsURL 把相对地址补成绝对地址。
func AbsURL(base, ref string) string {
	if ref == "" {
		return ""
	}
	u, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	if u.IsAbs() {
		return ref
	}
	b, err := url.Parse(base)
	if err != nil {
		return ref
	}
	return b.ResolveReference(u).String()
}

// Clean 统一清理文本：去首尾空白、去换行、NBSP 转空格。
func Clean(text string) string {
	text = strings.TrimSpace(text)
	text = strings.ReplaceAll(text, "\n", "")
	text = strings.ReplaceAll(text, "\r", "")
	text = strings.ReplaceAll(text, "\u00a0", " ")
	return text
}

func nodeText(node *html.Node) string {
	if node.Type == html.TextNode {
		return Clean(node.Data)
	}
	return Clean(htmlquery.InnerText(node))
}

// Actors 把名字列表包装成出演者；空名丢弃，缺省女性。
func Actors(names []string) []model.FilmActor {
	out := make([]model.FilmActor, 0, len(names))
	for _, name := range names {
		if name = Clean(name); name != "" {
			out = append(out, model.FilmActor{Name: name, Gender: model.GenderFemale})
		}
	}
	return out
}

func xpathFind(node *html.Node, expr string) []*html.Node { return htmlquery.Find(node, expr) }

func attrOf(node *html.Node, attr string) string {
	return strings.TrimSpace(htmlquery.SelectAttr(node, attr))
}

func nodeTextOf(node *html.Node) string { return nodeText(node) }
