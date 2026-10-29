// Package model 定义精简版的核心数据类型。
//
// 只保留刮削与整理需要的字段：番号、内容类型、文件相位、单站元数据与聚合结果。
package model

// ContentType 是番号的属性（不是文件的属性），决定刮削路由。
type ContentType string

const (
	Censored   ContentType = "censored"
	Uncensored ContentType = "uncensored"
	Chinese    ContentType = "chinese"
	Western    ContentType = "western"
	FC2        ContentType = "fc2"
	Amateur    ContentType = "amateur"
	Hentai     ContentType = "hentai"
)

// ContentTypeValues 按声明顺序排列，供配置校验与前端下拉使用。
var ContentTypeValues = []ContentType{Censored, Uncensored, Chinese, Western, FC2, Amateur, Hentai}

// Mosaic 是文件名/目录命中的马赛克标记；有码是条目类型，不在此列。
type Mosaic string

const (
	MosaicUncensored Mosaic = "uncensored"
	MosaicCracked    Mosaic = "cracked"
	MosaicLeaked     Mosaic = "leaked"
)

var MosaicValues = []Mosaic{MosaicUncensored, MosaicCracked, MosaicLeaked}

// Gender 与 Amane 的 ActorGender 对齐：多数出演栏是女优，缺省 unknown。
type Gender string

const (
	GenderFemale  Gender = "female"
	GenderMale    Gender = "male"
	GenderUnknown Gender = "unknown"
)

// FilmActor 是影片出演者。Name 已经过清洗，展示名不含别名括号。
type FilmActor struct {
	Name   string `json:"name"`
	Gender Gender `json:"gender"`
}

// SearchQuery 是传给站点爬虫的查询。FileHash 只在站点声明 uses_file_hash 时使用。
type SearchQuery struct {
	Number      string
	FilePath    string
	FileHash    string
	ContentType ContentType
}

// MediaMetadata 是单个站点的刮削结果。空值一律留空，由聚合层决定取舍。
type MediaMetadata struct {
	Number      string      `json:"number"`
	Title       string      `json:"title,omitempty"`
	Actors      []FilmActor `json:"actors,omitempty"`
	Studio      string      `json:"studio,omitempty"`
	Publisher   string      `json:"publisher,omitempty"`
	Release     string      `json:"release,omitempty"`
	Runtime     int         `json:"runtime,omitempty"`
	Tags        []string    `json:"tags,omitempty"`
	Series      string      `json:"series,omitempty"`
	Plot        string      `json:"plot,omitempty"`
	PosterURLs  []string    `json:"poster_urls,omitempty"`
	ThumbURLs   []string    `json:"thumb_urls,omitempty"`
	TrailerURLs []string    `json:"trailer_urls,omitempty"`
	Extrafanart []string    `json:"extrafanart,omitempty"`
	Score       float64     `json:"score,omitempty"`
	Directors   []string    `json:"directors,omitempty"`
	ExternalID  string      `json:"external_id,omitempty"`
	SourceURL   string      `json:"source_url,omitempty"`
}

// Aggregated 是多源聚合结果：标量取站点链首个非空，列表与图片按字段链拼接。
type Aggregated struct {
	MediaMetadata
	// FieldSources 记录单源字段的来源站点，调试与前端展示用。
	FieldSources map[string]string `json:"field_sources,omitempty"`
	// Sites 是本次实际参与聚合的站点顺序。
	Sites []string `json:"sites,omitempty"`
}

// FileInfo 是文件名/路径解析结果。类型与文件相位分离：类型属于番号，相位属于这一份文件。
type FileInfo struct {
	Number            string      `json:"number,omitempty"`
	ContentType       ContentType `json:"content_type"`
	Prefix            string      `json:"prefix,omitempty"`
	CD                int         `json:"cd,omitempty"`
	HasSubtitleInName bool        `json:"has_subtitle_in_name"`
	HasExternalSub    bool        `json:"has_external_subtitle"`
	HasSubtitle       bool        `json:"has_subtitle"`
	Mosaic            Mosaic      `json:"mosaic,omitempty"`
	Definition        string      `json:"definition,omitempty"`
}
