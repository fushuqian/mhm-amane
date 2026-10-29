// Package store 是精简版的 SQLite 存储：元数据、文件索引与任务队列。
//
// 与 Amane 的库不兼容：表结构按精简版重新设计，图片/列表字段以 JSON 列保存。
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/fushuqian/amane-lite/internal/model"
)

// ErrMissing 表示条目不存在。
var ErrMissing = errors.New("not found")

type Store struct{ db *sql.DB }

// MetaRecord 是库内的一条影片条目。
type MetaRecord struct {
	Number      string                     `json:"number"`
	ContentType model.ContentType          `json:"content_type"`
	Title       string                     `json:"title"`
	Plot        string                     `json:"plot"`
	Studio      string                     `json:"studio"`
	Publisher   string                     `json:"publisher"`
	Series      string                     `json:"series"`
	Release     string                     `json:"release"`
	Runtime     int                        `json:"runtime"`
	Score       float64                    `json:"score"`
	SourceURL   string                     `json:"source_url"`
	ExternalID  string                     `json:"external_id"`
	Actors      []model.FilmActor          `json:"actors"`
	Tags        []string                   `json:"tags"`
	Directors   []string                   `json:"directors"`
	PosterURLs  []string                   `json:"poster_urls"`
	ThumbURLs   []string                   `json:"thumb_urls"`
	TrailerURLs []string                   `json:"trailer_urls"`
	Extrafanart []string                   `json:"extrafanart"`
	FieldSource map[string]string          `json:"field_sources"`
	Sites       []string                   `json:"sites"`
	Raw         map[string]json.RawMessage `json:"raw"`
	CreatedAt   string                     `json:"created_at"`
	UpdatedAt   string                     `json:"updated_at"`
}

// ListItem 是列表页需要的精简行。
type ListItem struct {
	Number      string            `json:"number"`
	Title       string            `json:"title"`
	ContentType model.ContentType `json:"content_type"`
	Poster      string            `json:"poster"`
	Release     string            `json:"release,omitempty"`
	UpdatedAt   string            `json:"updated_at"`
}

// MediaFileRow 是文件索引行。
type MediaFileRow struct {
	Path        string `json:"path"`
	Number      string `json:"number"`
	CD          int    `json:"cd,omitempty"`
	Mosaic      string `json:"mosaic,omitempty"`
	HasSubtitle bool   `json:"has_subtitle"`
	Definition  string `json:"definition,omitempty"`
	Size        int64  `json:"size"`
}

// TaskRow 是任务队列行。
type TaskRow struct {
	ID        int64  `json:"id"`
	Kind      string `json:"kind"`
	Payload   string `json:"payload"`
	Status    string `json:"status"`
	Result    string `json:"result,omitempty"`
	Error     string `json:"error,omitempty"`
	Progress  string `json:"progress,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// Open 打开数据库并建表。
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	dsn := "file:" + path + "?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS metadata (
  number TEXT PRIMARY KEY,
  content_type TEXT NOT NULL,
  title TEXT, plot TEXT, studio TEXT, publisher TEXT, series TEXT,
  release TEXT, runtime INTEGER, score REAL,
  source_url TEXT, external_id TEXT,
  actors TEXT, tags TEXT, directors TEXT,
  poster_urls TEXT, thumb_urls TEXT, trailer_urls TEXT, extrafanart TEXT,
  field_sources TEXT, sites TEXT, raw TEXT,
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS media_files (
  path TEXT PRIMARY KEY,
  number TEXT,
  cd INTEGER NOT NULL DEFAULT 0,
  mosaic TEXT, has_subtitle INTEGER NOT NULL DEFAULT 0, definition TEXT,
  size INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_media_number ON media_files(number);
CREATE TABLE IF NOT EXISTS tasks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  kind TEXT NOT NULL,
  payload TEXT NOT NULL,
  status TEXT NOT NULL,
  result TEXT, error TEXT, progress TEXT,
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
`)
	if err != nil {
		return err
	}
	// 兼容旧库：media_files 早期没有 cd 列。
	return s.ensureColumn("media_files", "cd", "INTEGER NOT NULL DEFAULT 0")
}

// ensureColumn 在列不存在时补加，避免旧库升级后查询失败。
func (s *Store) ensureColumn(table, column, definition string) error {
	rows, err := s.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	exists := false
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == column {
			exists = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if exists {
		return nil
	}
	_, err = s.db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + definition)
	return err
}

// SaveMetadata 写入一条聚合结果（覆盖旧值），raw 按站点保存。
func (s *Store) SaveMetadata(agg model.Aggregated, ctype model.ContentType, raw map[string]json.RawMessage) error {
	now := time.Now().Format(time.RFC3339)
	actors, _ := json.Marshal(agg.Actors)
	tags, _ := json.Marshal(agg.Tags)
	directors, _ := json.Marshal(agg.Directors)
	posters, _ := json.Marshal(agg.PosterURLs)
	thumbs, _ := json.Marshal(agg.ThumbURLs)
	trailers, _ := json.Marshal(agg.TrailerURLs)
	art, _ := json.Marshal(agg.Extrafanart)
	sources, _ := json.Marshal(agg.FieldSources)
	sitesJSON, _ := json.Marshal(agg.Sites)
	rawJSON, _ := json.Marshal(raw)
	_, err := s.db.Exec(`
INSERT INTO metadata (number, content_type, title, plot, studio, publisher, series, release, runtime, score,
  source_url, external_id, actors, tags, directors, poster_urls, thumb_urls, trailer_urls, extrafanart,
  field_sources, sites, raw, created_at, updated_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(number) DO UPDATE SET
  content_type=excluded.content_type, title=excluded.title, plot=excluded.plot, studio=excluded.studio,
  publisher=excluded.publisher, series=excluded.series, release=excluded.release, runtime=excluded.runtime,
  score=excluded.score, source_url=excluded.source_url, external_id=excluded.external_id, actors=excluded.actors,
  tags=excluded.tags, directors=excluded.directors, poster_urls=excluded.poster_urls, thumb_urls=excluded.thumb_urls,
  trailer_urls=excluded.trailer_urls, extrafanart=excluded.extrafanart, field_sources=excluded.field_sources,
  sites=excluded.sites, raw=excluded.raw, updated_at=excluded.updated_at`,
		agg.Number, string(ctype), agg.Title, agg.Plot, agg.Studio, agg.Publisher, agg.Series, agg.Release,
		agg.Runtime, agg.Score, agg.SourceURL, agg.ExternalID, string(actors), string(tags), string(directors),
		string(posters), string(thumbs), string(trailers), string(art), string(sources), string(sitesJSON),
		string(rawJSON), now, now)
	return err
}

const metaColumns = `number, content_type, title, coalesce(plot,''), coalesce(studio,''), coalesce(publisher,''),
coalesce(series,''), coalesce(release,''), coalesce(runtime,0), coalesce(score,0), coalesce(source_url,''),
coalesce(external_id,''), coalesce(actors,'[]'), coalesce(tags,'[]'), coalesce(directors,'[]'),
coalesce(poster_urls,'[]'), coalesce(thumb_urls,'[]'), coalesce(trailer_urls,'[]'), coalesce(extrafanart,'[]'),
coalesce(field_sources,'{}'), coalesce(sites,'[]'), coalesce(raw,'{}'), created_at, updated_at`

// GetMetadata 按番号读取条目。
func (s *Store) GetMetadata(number string) (*MetaRecord, error) {
	row := s.db.QueryRow(`SELECT `+metaColumns+` FROM metadata WHERE number = ?`, number)
	return scanMeta(row)
}

// ListMetadata 分页列出条目，支持按标题/番号模糊搜索。
func (s *Store) ListMetadata(offset, limit int, query string) ([]ListItem, int, error) {
	if limit <= 0 || limit > 200 {
		limit = 40
	}
	where := ""
	args := []any{}
	if strings.TrimSpace(query) != "" {
		where = " WHERE number LIKE ? OR title LIKE ?"
		like := "%" + strings.TrimSpace(query) + "%"
		args = append(args, like, like)
	}
	var total int
	if err := s.db.QueryRow(`SELECT count(*) FROM metadata`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(`SELECT number, coalesce(title,''), content_type, coalesce(release,''), updated_at,
		coalesce(poster_urls,'[]') FROM metadata`+where+` ORDER BY created_at DESC, number DESC LIMIT ? OFFSET ?`,
		append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]ListItem, 0, limit)
	for rows.Next() {
		var item ListItem
		var posters string
		if err := rows.Scan(&item.Number, &item.Title, &item.ContentType, &item.Release, &item.UpdatedAt, &posters); err != nil {
			return nil, 0, err
		}
		var list []string
		_ = json.Unmarshal([]byte(posters), &list)
		if len(list) > 0 {
			item.Poster = list[0]
		}
		out = append(out, item)
	}
	return out, total, rows.Err()
}

// DeleteMetadata 删除条目及其文件索引，不删磁盘文件。
func (s *Store) DeleteMetadata(number string) error {
	res, err := s.db.Exec(`DELETE FROM metadata WHERE number = ?`, number)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrMissing
	}
	_, err = s.db.Exec(`UPDATE media_files SET number = NULL WHERE number = ?`, number)
	return err
}

func scanMeta(row *sql.Row) (*MetaRecord, error) {
	var rec MetaRecord
	var ctype, actors, tags, directors, posters, thumbs, trailers, art, sources, sitesJSON, rawJSON string
	err := row.Scan(&rec.Number, &ctype, &rec.Title, &rec.Plot, &rec.Studio, &rec.Publisher, &rec.Series,
		&rec.Release, &rec.Runtime, &rec.Score, &rec.SourceURL, &rec.ExternalID, &actors, &tags, &directors,
		&posters, &thumbs, &trailers, &art, &sources, &sitesJSON, &rawJSON, &rec.CreatedAt, &rec.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrMissing
	}
	if err != nil {
		return nil, err
	}
	rec.ContentType = model.ContentType(ctype)
	_ = json.Unmarshal([]byte(actors), &rec.Actors)
	_ = json.Unmarshal([]byte(tags), &rec.Tags)
	_ = json.Unmarshal([]byte(directors), &rec.Directors)
	_ = json.Unmarshal([]byte(posters), &rec.PosterURLs)
	_ = json.Unmarshal([]byte(thumbs), &rec.ThumbURLs)
	_ = json.Unmarshal([]byte(trailers), &rec.TrailerURLs)
	_ = json.Unmarshal([]byte(art), &rec.Extrafanart)
	_ = json.Unmarshal([]byte(sources), &rec.FieldSource)
	_ = json.Unmarshal([]byte(sitesJSON), &rec.Sites)
	_ = json.Unmarshal([]byte(rawJSON), &rec.Raw)
	return &rec, nil
}

// RegisteredMediaFile 登记一条文件索引；重复路径覆盖。
func (s *Store) RegisterMediaFile(path string, info model.FileInfo, size int64) error {
	hasSub := 0
	if info.HasSubtitle {
		hasSub = 1
	}
	_, err := s.db.Exec(`
INSERT INTO media_files (path, number, cd, mosaic, has_subtitle, definition, size, created_at)
VALUES (?,?,?,?,?,?,?,?)
ON CONFLICT(path) DO UPDATE SET number=excluded.number, cd=excluded.cd, mosaic=excluded.mosaic,
  has_subtitle=excluded.has_subtitle, definition=excluded.definition, size=excluded.size`,
		path, nullText(info.Number), info.CD, nullText(string(info.Mosaic)), hasSub, nullText(info.Definition), size,
		time.Now().Format(time.RFC3339))
	return err
}

func (s *Store) ListMediaFiles(number string) ([]MediaFileRow, error) {
	rows, err := s.db.Query(`SELECT path, coalesce(number,''), coalesce(cd,0), coalesce(mosaic,''), has_subtitle,
		coalesce(definition,''), size FROM media_files WHERE number = ? ORDER BY cd, path`, number)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MediaFileRow{}
	for rows.Next() {
		var row MediaFileRow
		var hasSub int
		if err := rows.Scan(&row.Path, &row.Number, &row.CD, &row.Mosaic, &hasSub, &row.Definition, &row.Size); err != nil {
			return nil, err
		}
		row.HasSubtitle = hasSub == 1
		out = append(out, row)
	}
	return out, rows.Err()
}

// UpdateMediaFilePath 整理（移动）成功后把索引里的源路径改成片库路径。
func (s *Store) UpdateMediaFilePath(oldPath, newPath string) error {
	if oldPath == newPath {
		return nil
	}
	// 新路径已存在时先清掉旧行，避免主键冲突。
	if _, err := s.db.Exec(`DELETE FROM media_files WHERE path = ? AND path != ?`, newPath, oldPath); err != nil {
		return err
	}
	_, err := s.db.Exec(`UPDATE media_files SET path = ? WHERE path = ?`, newPath, oldPath)
	return err
}

// CreateTask 入队一个任务，返回 id。
func (s *Store) CreateTask(kind string, payload any) (int64, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	now := time.Now().Format(time.RFC3339)
	res, err := s.db.Exec(`INSERT INTO tasks (kind, payload, status, created_at, updated_at) VALUES (?,?,?,?,?)`,
		kind, string(data), "queued", now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ClaimTask 取出最早排队的一条并标记为 running；没有任务返回 nil。
func (s *Store) ClaimTask() (*TaskRow, error) {
	row := s.db.QueryRow(`SELECT id, kind, payload, status, coalesce(result,''), coalesce(error,''),
		coalesce(progress,''), created_at, updated_at FROM tasks WHERE status='queued' ORDER BY id LIMIT 1`)
	var task TaskRow
	err := row.Scan(&task.ID, &task.Kind, &task.Payload, &task.Status, &task.Result, &task.Error,
		&task.Progress, &task.CreatedAt, &task.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err := s.db.Exec(`UPDATE tasks SET status='running', updated_at=? WHERE id=? AND status='queued'`,
		time.Now().Format(time.RFC3339), task.ID); err != nil {
		return nil, err
	}
	task.Status = "running"
	return &task, nil
}

// FinishTask 写入终态与结果。
func (s *Store) FinishTask(id int64, status, result, errText string) error {
	_, err := s.db.Exec(`UPDATE tasks SET status=?, result=?, error=?, updated_at=? WHERE id=?`,
		status, result, errText, time.Now().Format(time.RFC3339), id)
	return err
}

// UpdateProgress 写入进度文本。
func (s *Store) UpdateProgress(id int64, progress string) error {
	_, err := s.db.Exec(`UPDATE tasks SET progress=?, updated_at=? WHERE id=?`,
		progress, time.Now().Format(time.RFC3339), id)
	return err
}

// ListTasks 返回最近任务。
func (s *Store) ListTasks(limit int) ([]TaskRow, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return s.queryTasks(`SELECT `+taskColumns+` FROM tasks ORDER BY id DESC LIMIT ?`, limit)
}

// ListActiveTasks 只返回排队中与运行中的任务，供「任务」页轮询。
func (s *Store) ListActiveTasks(limit int) ([]TaskRow, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return s.queryTasks(`SELECT `+taskColumns+` FROM tasks WHERE status IN ('queued','running')
		ORDER BY id DESC LIMIT ?`, limit)
}

// HasActive 判断某类任务是否还在排队或运行，用于定时调度去重。
func (s *Store) HasActive(kind string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT count(*) FROM tasks WHERE kind = ? AND status IN ('queued','running')`, kind).Scan(&n)
	return n > 0, err
}

const taskColumns = `id, kind, payload, status, coalesce(result,''), coalesce(error,''),
	coalesce(progress,''), created_at, updated_at`

func (s *Store) queryTasks(query string, args ...any) ([]TaskRow, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TaskRow{}
	for rows.Next() {
		var t TaskRow
		if err := rows.Scan(&t.ID, &t.Kind, &t.Payload, &t.Status, &t.Result, &t.Error, &t.Progress,
			&t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ResetRunning 把进程退出时残留的 running 任务放回队列。
func (s *Store) ResetRunning() error {
	_, err := s.db.Exec(`UPDATE tasks SET status='queued', updated_at=? WHERE status='running'`,
		time.Now().Format(time.RFC3339))
	return err
}

// Stats 返回首页卡片需要的计数。
func (s *Store) Stats() (map[string]int, error) {
	out := map[string]int{}
	for key, query := range map[string]string{
		"metadata": `SELECT count(*) FROM metadata`,
		"files":    `SELECT count(*) FROM media_files`,
		"tasks":    `SELECT count(*) FROM tasks WHERE status IN ('queued','running')`,
	} {
		var n int
		if err := s.db.QueryRow(query).Scan(&n); err != nil {
			return nil, err
		}
		out[key] = n
	}
	return out, nil
}

func nullText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// Ensure 用于测试与首次启动时校验目录。
func Ensure(path string) error { return os.MkdirAll(path, 0o755) }

// DatabasePath 返回数据目录下的库文件路径。
func DatabasePath(dataDir string) string { return filepath.Join(dataDir, "amane.db") }
