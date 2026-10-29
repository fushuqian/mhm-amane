// Package config 保存精简版的运行配置，落在数据目录的 config.json。
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// 默认值与固定项。
const (
	DefaultMediaRoot     = "/media"
	DefaultDownloadRoot  = "/downloads"
	DefaultVideoTemplate = "{actress}/{number}[-CD{cd?}][-{sub?}].{ext}"
	MihomoProxyURL       = "http://127.0.0.1:7890"

	ModeCopy = "copy"
	ModeMove = "move"
)

// SiteSetting 是单个站点的运行期覆盖项。
type SiteSetting struct {
	BaseURL string            `json:"base_url,omitempty"`
	Cookies map[string]string `json:"cookies,omitempty"`
}

// DownloadDir 是一个下载库路径；整理方式按目录区分（PT 用复制，迅雷用移动）。
type DownloadDir struct {
	Path string `json:"path"`
	Mode string `json:"mode"` // copy / move
}

// Library 是整理参数：片库只有一个，模板只写相对片库根的部分。
type Library struct {
	Root          string `json:"root"`
	VideoTemplate string `json:"video_template"`
	WriteNFO      bool   `json:"write_nfo"`
	WriteImages   bool   `json:"write_images"`
}

// Automation 是定时自动扫描、刮削、整理。
type Automation struct {
	Enabled         bool `json:"enabled"`
	IntervalMinutes int  `json:"interval_minutes"`
	RunOnStart      bool `json:"run_on_start"`
}

// Config 是完整配置。
type Config struct {
	Addr         string                 `json:"addr"`
	UseMihomo    bool                   `json:"use_mihomo_proxy"`
	Sites        []string               `json:"sites"`
	SiteSettings map[string]SiteSetting `json:"site_settings,omitempty"`
	Library      Library                `json:"library"`
	DownloadDirs []DownloadDir          `json:"download_dirs"`
	Automation   Automation             `json:"automation"`
}

// EffectiveProxy 返回当前应当使用的代理地址；关闭开关即直连。
func (c Config) EffectiveProxy() string {
	if c.UseMihomo {
		return MihomoProxyURL
	}
	return ""
}

// Default 返回默认配置。mediaRoot 为空时用 /media；downloadRoot 存在时才作为初始下载目录。
func Default(mediaRoot, downloadRoot string) Config {
	if strings.TrimSpace(mediaRoot) == "" {
		mediaRoot = DefaultMediaRoot
	}
	if strings.TrimSpace(downloadRoot) == "" {
		downloadRoot = DefaultDownloadRoot
	}
	cfg := Config{
		Addr:  "127.0.0.1:8088",
		Sites: []string{"javbus", "mgstage"},
		Library: Library{
			Root:          mediaRoot,
			VideoTemplate: DefaultVideoTemplate,
			WriteNFO:      true,
			WriteImages:   true,
		},
		Automation: Automation{Enabled: true, IntervalMinutes: 30, RunOnStart: true},
	}
	if downloadRoot != mediaRoot && dirExists(downloadRoot) {
		cfg.DownloadDirs = []DownloadDir{{Path: downloadRoot, Mode: ModeCopy}}
	}
	return cfg
}

// legacy 是 v1 配置的兼容读取结构：模板两段式、mode 四种、proxy 自由填写。
type legacy struct {
	Proxy     string `json:"proxy"`
	MediaRoot string `json:"media_root"`
	Library   struct {
		Root         string `json:"root"`
		Mode         string `json:"mode"`
		DirTemplate  string `json:"dir_template"`
		FileTemplate string `json:"file_template"`
	} `json:"library"`
}

// Load 读取配置；文件不存在时写入默认值，旧版本字段自动迁移。
func Load(path, mediaRoot, downloadRoot string) (Config, error) {
	cfg := Default(mediaRoot, downloadRoot)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		cfg.Normalize()
		return cfg, Save(path, cfg)
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}

	var old legacy
	_ = json.Unmarshal(data, &old)
	var raw struct {
		Library map[string]json.RawMessage `json:"library"`
	}
	_ = json.Unmarshal(data, &raw)
	_, hasVideoTemplate := raw.Library["video_template"]
	if !hasVideoTemplate && old.Library.DirTemplate != "" {
		dir := strings.TrimSpace(old.Library.DirTemplate)
		file := strings.TrimSpace(old.Library.FileTemplate)
		if file == "" {
			file = "{number}"
		}
		cfg.Library.VideoTemplate = strings.TrimSuffix(dir, "/") + "/" + file + ".{ext}"
	}
	if old.Proxy != "" && !cfg.UseMihomo && strings.Contains(old.Proxy, "7890") {
		cfg.UseMihomo = true
	}

	cfg.Normalize()
	return cfg, nil
}

// Normalize 归一化：补默认值、修正非法整理方式、去重下载目录、剥离模板里的片库前缀。
func (c *Config) Normalize() {
	c.Addr = strings.TrimSpace(c.Addr)
	if c.Addr == "" {
		c.Addr = "127.0.0.1:8088"
	}
	if len(c.Sites) == 0 {
		c.Sites = []string{"javbus", "mgstage"}
	}

	c.Library.Root = strings.TrimSpace(c.Library.Root)
	if c.Library.Root == "" {
		c.Library.Root = DefaultMediaRoot
	}
	c.Library.VideoTemplate = strings.TrimSpace(c.Library.VideoTemplate)
	c.Library.VideoTemplate = StripRootPrefix(c.Library.Root, c.Library.VideoTemplate)
	if c.Library.VideoTemplate == "" {
		c.Library.VideoTemplate = DefaultVideoTemplate
	}

	if c.Automation.IntervalMinutes <= 0 {
		c.Automation.IntervalMinutes = 30
	}
	if c.Automation.IntervalMinutes < 5 {
		c.Automation.IntervalMinutes = 5
	}
	if c.Automation.IntervalMinutes > 1440 {
		c.Automation.IntervalMinutes = 1440
	}

	seen := map[string]struct{}{}
	out := make([]DownloadDir, 0, len(c.DownloadDirs))
	for _, dir := range c.DownloadDirs {
		dir.Path = strings.TrimSpace(dir.Path)
		if dir.Path == "" {
			continue
		}
		if dir.Mode != ModeMove {
			dir.Mode = ModeCopy
		}
		if _, ok := seen[dir.Path]; ok {
			continue
		}
		seen[dir.Path] = struct{}{}
		out = append(out, dir)
	}
	c.DownloadDirs = out
}

// Validate 校验关键字段；错误信息直接展示给用户。
func (c Config) Validate() error {
	if strings.TrimSpace(c.Library.Root) == "" {
		return errors.New("片库路径不能为空")
	}
	for i, dir := range c.DownloadDirs {
		if strings.TrimSpace(dir.Path) == "" {
			return fmt.Errorf("第 %d 个下载目录路径为空", i+1)
		}
		if dir.Path == c.Library.Root {
			return fmt.Errorf("下载目录不能与片库路径相同：%s", dir.Path)
		}
		if dir.Mode != ModeCopy && dir.Mode != ModeMove {
			return fmt.Errorf("整理方式只支持复制或移动：%s", dir.Mode)
		}
	}
	if c.Automation.IntervalMinutes < 5 || c.Automation.IntervalMinutes > 1440 {
		return errors.New("执行间隔需在 5–1440 分钟之间")
	}
	return nil
}

// StripRootPrefix 去掉模板里误贴的片库根前缀，以及开头多余的 /。
func StripRootPrefix(root, template string) string {
	t := strings.TrimSpace(template)
	root = strings.TrimSuffix(strings.TrimSpace(root), "/")
	if root != "" && strings.HasPrefix(t, root+"/") {
		t = strings.TrimPrefix(t, root+"/")
	}
	return strings.TrimPrefix(t, "/")
}

// Save 原子写回配置。
func Save(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
