# 更新日志

## 0.2.1 — 2026-10-30

- 整理时下载海报/剧照补上同源 Referer：javbus 等站点不再返回 403，`poster.jpg` / `thumb.jpg` 正常落地。

## 0.2.0 — 2026-10-30

- 配置升级到 v2：片库路径（1 个）+ 下载库路径（多个，每个独立整理方式）、自动任务（开关/间隔/启动执行）、mihomo 代理开关；旧 `dir_template + file_template` 自动迁移为视频路径模板，旧 `hardlink/symlink` 回落为 `copy`。
- 整理方式只保留「复制 / 移动」；按来源下载目录决定，PT 用复制、迅雷等临时下载用移动。
- 新增原版视频路径模板引擎：占位符、可选组 `[..]`、双层组 `[[..]]`、值映射 `{name|原值=输出}`、未知占位符输出 `Unknown`；模板只写相对片库根，片库路径由程序自动拼接，误贴 `/media/...` 会自动剥离。
- 新增自动链路 `sync`：扫描所有下载库 → 新番号刮削 → 按目录整理 →（移动后）回写文件索引；调度按配置间隔入队，启动时可选执行一次。
- 片库改名「新片速递」，按首次入库时间倒序，固定显示最新 10 条，去掉分页/加载更多。
- 「刮削记录」改名「任务」，只显示进行中的任务，每 2 秒轮询，去掉手动提交表单；详情页保留重新刮削/重新整理。
- 设置页改为六组折叠：常规设置、网络、刮削站点、目录管理、整理方式、视频路径模板。
- 网络分组只保留「使用 mihomo 代理（127.0.0.1:7890）」开关。
- 刮削站点分组：站点方块 + 连通性测试按钮（10 秒超时，绿=通、红=不通）。
- 目录管理新增文件浏览器：只允许浏览 `AMANE_SAFE_DIRS`（默认 `/media,/downloads`），片库默认 `/media`、下载库默认 `/downloads`。
- 新增接口：`GET /api/tasks?active=1`、`GET /api/fs?path=`、`POST /api/sites/check`；`PATCH /api/config` 增加路径范围与模板语法校验。
- 写出 NFO / 写出图片默认开启；图片已存在则跳过，避免复制模式每轮重复下载。
- manifest 新增 `AMANE_DOWNLOAD_DIR`、`AMANE_SAFE_DIRS`。

## 0.1.3 — 2026-10-30

- 修正 javbus / mgstage 的发行日期与时长解析：只取 `td` / `p` 的直接文本，不再把表头标签拼进值。
- 元数据 JSON 字段改为小写下划线，前端详情/编辑表单能正确取值。
- 图片代理带上同源 Referer，javbus 等站点不再 403。
- 端到端实测：模块内提交 javbus 刮削成功，标题、片商、演员、标签、剧照均入库。

## 0.1.2 — 2026-10-30

- 修正 manifest：权限声明放进 `sidecar.permissions`（此前只在顶层，宿主因此跳过了权限确认）。
- 安装到本地 Mica Home 实测通过：sidecar 走 Unix socket、`__status=running`、模块通道与健康检查正常。

## 0.1.1 — 2026-10-30

- 对齐 Mica Home f29bbc3：`healthTimeoutSec=10`、sidecar 通道校验 `X-Mica-Token`。
- 文档修正：宿主运行层已是 Debian（glibc）、sidecar 上限 512MB、反代已支持 WebSocket。

## 0.1.0 — 2026-10-30

- 新建 Go 精简版工程：番号解析、TLS 指纹 HTTP、站点注册表、聚合、SQLite、任务队列、整理落盘、REST API、Mica 风格前端与模块包。
- 首批站点：javbus、mgstage。
- 模块包构建脚本：静态 linux/amd64 sidecar + 前端 zip。
