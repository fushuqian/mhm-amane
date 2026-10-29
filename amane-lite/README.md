# Amane Lite

Mica Home 适配的精简版 Amane：**只做刮削与整理**。Go 单二进制、内嵌前端，不依赖 Python 运行时。

## 范围

**保留**
- 番号解析（文件名 → 番号 / 类型 / 分集 / 中字 / 马赛克 / 分辨率）
- 多源刮削与字段级择优（标量取首个非空，图片按站点拼接）
- 定时自动链路：扫描下载库 → 新番号刮削 → 按模板整理（30 分钟一次，可关）
- 一个片库 + 多个下载库；每个下载目录独立选择「复制 / 移动」
- 视频路径模板沿用原版语法（占位符 / 可选组 / 值映射）+ NFO + 海报/剧照
- 任务队列只展示进行中的任务；新片速递按首次入库时间倒序，固定 10 条
- Mica 风格精简 UI（新片速递 / 任务 / 设置）

**不做**
- AI 助理、插件、播放、RSS 订阅、演员页、分类治理、媒体服务器同步、超分与水印
- 硬链接 / 符号链接整理（宿主机 ↔ Docker 跨挂载不可用）

## 快速开始（开发）

```bash
go run ./cmd/amaned -data ./data -addr 127.0.0.1:8088 -media ./media -downloads ./downloads
```

打开 <http://127.0.0.1:8088/>。

## 构建 Mica Home 模块

```powershell
powershell -ExecutionPolicy Bypass -File scripts/build-module.ps1
```

产物：`dist/amane-<version>.zip`，根目录为 `manifest.json` + `frontend/` + `sidecar/`。
sidecar 是静态编译的 `linux/amd64` 二进制，宿主无需改基础镜像（运行层已是 Debian，纯 Go 静态二进制同样适用）。

### 宿主机挂载

模块把片库与下载库当成固定根：片库默认 `/media`，下载库默认 `/downloads`，路径选择器也只允许浏览 `AMANE_SAFE_DIRS`（默认 `/media,/downloads`）。compose 里加两条挂载即可：

```yaml
volumes:
  - /data:/data
  - /vol1/1000/media:/media
  - /vol1/1000/downloads:/downloads
```

没有挂载的根不会出现在文件浏览器里。

## 当前状态

- 已移植站点：`javbus`、`mgstage`
- 其余站点按 Amane 源码逐个移植，优先无 JS 挑战的站点
- 浏览器后端（内置 Chromium）尚未接线；宿主已改为 Debian（glibc）运行层，且 sidecar 上限 512MB，接线后可直接打包 Chrome headless

## 目录

```
cmd/amaned            服务入口
internal/number       番号解析与比对（移植自 Amane）
internal/fetch        TLS 指纹客户端、限速、拦截页识别
internal/sites        站点注册表与爬虫
internal/aggregate    多源字段聚合
internal/store        SQLite：元数据 / 文件索引 / 任务
internal/organize     模板落盘、NFO、图片
internal/worker       任务消费
internal/api          REST + 内嵌前端
module                模块包（manifest / frontend / sidecar）
```
