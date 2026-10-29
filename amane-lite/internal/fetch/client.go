// Package fetch 封装带浏览器指纹的 HTTP 客户端：TLS 指纹伪装、按站点限速、Cookie 会话与拦截页识别。
//
// 用 tls-client（Chrome 指纹）而不是标准 net/http：多数站点按 TLS/JA3 判定客户端，
// 纯 Go 默认指纹会被直接挡下。
package fetch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	fhttp "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
	"golang.org/x/net/html/charset"
	"golang.org/x/time/rate"
)

// ErrNotFound 表示站点明确没有该条目（404 或检索为空），调用方继续换下一个来源。
var ErrNotFound = errors.New("not found")

// ErrBlocked 表示命中 Cloudflare/年龄门等拦截页。
var ErrBlocked = errors.New("blocked by site")

// Options 是单次请求的可选项。
type Options struct {
	Headers map[string]string
	Cookies map[string]string
	// Referer 为空时使用站点自身 base（部分站点校验同源 Referer）。
	Referer string
}

// Response 是请求结果；Body 已按 UTF-8 解码（站点声明 charset 时按声明转码）。
type Response struct {
	Status int
	Body   string
	Header fhttp.Header
	URL    string
}

// Client 是共享的指纹 HTTP 客户端，按 host 做令牌桶限速。
type Client struct {
	http     tls_client.HttpClient
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
	rate     rate.Limit
	burst    int
}

// Config 控制指纹、代理与限速。
type Config struct {
	ProxyURL  string
	Timeout   time.Duration
	Rate      rate.Limit // 每秒请求数；0 表示默认 1
	Burst     int        // 0 表示默认 2
	Profile   *profiles.ClientProfile
	ChromeVer string
}

// New 创建客户端。profile 为空时使用 Chrome_131。
func New(cfg Config) (*Client, error) {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 45 * time.Second
	}
	if cfg.Rate <= 0 {
		cfg.Rate = 1
	}
	if cfg.Burst <= 0 {
		cfg.Burst = 2
	}
	profile := profiles.Chrome_131
	if cfg.Profile != nil {
		profile = *cfg.Profile
	}
	jar := tls_client.NewCookieJar()
	opts := []tls_client.HttpClientOption{
		tls_client.WithTimeoutSeconds(int(cfg.Timeout.Seconds())),
		tls_client.WithClientProfile(profile),
		tls_client.WithCookieJar(jar),
		tls_client.WithRandomTLSExtensionOrder(),
	}
	if cfg.ProxyURL != "" {
		opts = append(opts, tls_client.WithProxyUrl(cfg.ProxyURL))
	}
	client, err := tls_client.NewHttpClient(tls_client.NewNoopLogger(), opts...)
	if err != nil {
		return nil, fmt.Errorf("tls client: %w", err)
	}
	return &Client{
		http:     client,
		limiters: map[string]*rate.Limiter{},
		rate:     cfg.Rate,
		burst:    cfg.Burst,
	}, nil
}

// SetProxy 运行期切换代理；空串表示直连。
func (c *Client) SetProxy(proxyURL string) error {
	return c.http.SetProxy(proxyURL)
}

// GetHTML 发起 GET 并返回响应；404 转 ErrNotFound，拦截页转 ErrBlocked。
func (c *Client) GetHTML(ctx context.Context, rawURL string, opts Options) (*Response, error) {
	raw, err := c.do(ctx, fhttp.MethodGet, rawURL, nil, opts)
	if err != nil {
		return nil, err
	}
	defer raw.Body.Close()
	resp, err := decodeResponse(raw)
	if err != nil {
		return nil, err
	}
	switch {
	case raw.StatusCode == 404:
		return resp, ErrNotFound
	case raw.StatusCode >= 400:
		return resp, fmt.Errorf("http %d: %s", raw.StatusCode, rawURL)
	}
	if reason := classifyBlock(resp.Body); reason != "" {
		return resp, fmt.Errorf("%w: %s", ErrBlocked, reason)
	}
	return resp, nil
}

// GetJSON 直接返回原始响应体，不做 HTML 拦截启发式。
func (c *Client) GetJSON(ctx context.Context, rawURL string, opts Options) (*Response, error) {
	raw, err := c.do(ctx, fhttp.MethodGet, rawURL, nil, opts)
	if err != nil {
		return nil, err
	}
	defer raw.Body.Close()
	resp, err := decodeResponse(raw)
	if err != nil {
		return nil, err
	}
	if raw.StatusCode >= 400 {
		return resp, fmt.Errorf("http %d: %s", raw.StatusCode, rawURL)
	}
	return resp, nil
}

// Download 下载二进制到 dest；失败不留下半截文件。
func (c *Client) Download(ctx context.Context, rawURL, dest string, opts Options) error {
	raw, err := c.do(ctx, fhttp.MethodGet, rawURL, nil, opts)
	if err != nil {
		return err
	}
	defer raw.Body.Close()
	if raw.StatusCode >= 400 {
		return fmt.Errorf("http %d: %s", raw.StatusCode, rawURL)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp := dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, raw.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dest)
}

// GetBytes 下载二进制并返回内容类型（图片代理用）。
func (c *Client) GetBytes(ctx context.Context, rawURL string, opts Options) ([]byte, string, error) {
	raw, err := c.do(ctx, fhttp.MethodGet, rawURL, nil, opts)
	if err != nil {
		return nil, "", err
	}
	defer raw.Body.Close()
	if raw.StatusCode >= 400 {
		return nil, "", fmt.Errorf("http %d: %s", raw.StatusCode, rawURL)
	}
	data, err := io.ReadAll(raw.Body)
	if err != nil {
		return nil, "", err
	}
	return data, raw.Header.Get("Content-Type"), nil
}

// decodeResponse 读取响应体，并按 Content-Type 声明的字符集转成 UTF-8。
func decodeResponse(raw *fhttp.Response) (*Response, error) {
	data, err := io.ReadAll(raw.Body)
	if err != nil {
		return nil, err
	}
	body := string(data)
	if ct := raw.Header.Get("Content-Type"); ct != "" {
		if decoded, derr := charset.NewReader(bytes.NewReader(data), ct); derr == nil {
			if b, rerr := io.ReadAll(decoded); rerr == nil {
				body = string(b)
			}
		}
	}
	return &Response{Status: raw.StatusCode, Body: body, Header: raw.Header, URL: raw.Request.URL.String()}, nil
}

func (c *Client) do(ctx context.Context, method, rawURL string, body io.Reader, opts Options) (*fhttp.Response, error) {
	limiter := c.limiterFor(rawURL)
	if err := limiter.Wait(ctx); err != nil {
		return nil, err
	}
	req, err := fhttp.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, err
	}
	for k, v := range opts.Headers {
		req.Header.Set(k, v)
	}
	if opts.Referer != "" {
		req.Header.Set("Referer", opts.Referer)
	}
	for k, v := range opts.Cookies {
		req.AddCookie(&fhttp.Cookie{Name: k, Value: v})
	}
	if req.Header.Get("User-Agent") == "" {
		// tls-client 会按 profile 覆盖 UA，这里只兜底。
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	}
	return c.http.Do(req)
}

func (c *Client) limiterFor(rawURL string) *rate.Limiter {
	host := rawURL
	if u, err := url.Parse(rawURL); err == nil && u.Host != "" {
		host = u.Host
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	l, ok := c.limiters[host]
	if !ok {
		l = rate.NewLimiter(c.rate, c.burst)
		c.limiters[host] = l
	}
	return l
}

// classifyBlock 识别常见拦截页；返回空串表示正常页面。
func classifyBlock(body string) string {
	lower := strings.ToLower(body)
	switch {
	case strings.Contains(lower, "just a moment") && strings.Contains(lower, "cloudflare"):
		return "cloudflare challenge"
	case strings.Contains(lower, "cf-chl") || strings.Contains(lower, "challenge-platform"):
		return "cloudflare challenge"
	case strings.Contains(lower, "turnstile") && strings.Contains(lower, "cf-"):
		return "cloudflare turnstile"
	case strings.Contains(body, "年齢確認") && len(body) < 4096:
		return "age gate"
	}
	return ""
}
