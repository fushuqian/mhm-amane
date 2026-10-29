# Mica Home × Amane：可行性总结

> **状态更新（2026-10-30）**：方案已改为 **Go 精简版重写**（`d:\Trae\mhm-amane\amane-lite`），只保留番号解析、刮削与整理，不再打包 Python 运行时。
> 宿主侧三项需求已在 Mica Home `f29bbc3` 全部实现（Debian/glibc 运行层、512MB sidecar 限额、WebSocket 反代），因此原先针对 Python 方案的宿主要求已不适用。
> 本文以下内容保留为可行性判断的历史记录；最新对接状态见 `d:\Trae\mhm-amane\MicaHome-Amane模块-宿主支持需求.md`。

> 交付目标：判断"能否做成 Mica Home 模块"、卡点与代价，并把宿主改动整理成可直接转给对方的需求。不含实施细节。

## 1. 结论

**可行，但前提是 Mica Home 宿主做 3 项改动，否则装不进去。**

* 推荐形态：模块 = 简化前端 + `amane-watch` sidecar（内含 Python/Amane 运行时）。

* 不改 Amane 源码，不从源码编译，运行时直接取官方镜像产物。

* 首版功能定为：刮削、整理、翻译、路径设置、刮削记录。

* 若不改宿主，最多只能做成"卡片 + 新窗口跳转"的入口模块，不属于真正集成。

**可行性评级**：技术路径清晰，无不可解问题；主要代价在宿主改动与约 300MB 的 sidecar 体积。

## 2. 技术盘点（实测，非推测）

| 项              | 事实                                                                   | 来源                                                                        |
| -------------- | -------------------------------------------------------------------- | ------------------------------------------------------------------------- |
| Mica Home 运行环境 | Go 静态二进制 + `alpine:3.19`（musl）                                       | [Dockerfile](file:///d:/Trae/mica_home/Dockerfile)                        |
| 模块机制           | `manifest.json` + `frontend/`（纯 ESM，`mount(el, api)` / `renderCard`） | [模块开发文档](http://localhost:8080/docs/modules)                              |
| sidecar        | 模块包内原生可执行文件，宿主 exec 拉起，注入 `MICA_*`，5 秒健康检查，退避重启                      | [manager.go](file:///d:/Trae/mica_home/internal/sidecar/manager.go)       |
| 体积限额           | 单包 160MB / 前端 20MB / **sidecar 解压 128MB**                            | [module.go](file:///d:/Trae/mica_home/internal/module/module.go#L29-L36)  |
| 反代             | `http.Client + io.Copy`，支持流式，但丢弃 `Upgrade` → **不支持 WebSocket**       | [host.go](file:///d:/Trae/mica_home/internal/host/host.go#L400-L444)      |
| Amane 服务       | Python 3.14 + FastAPI + SQLite，`python -m amane.server`              | [pyproject.toml](file:///d:/Trae/mhm-amane/amane-repo/pyproject.toml)     |
| Amane 镜像       | 630MB；`/app` 215MB（venv 206MB）；Python 运行库 41MB                       | `docker image inspect` / `du`                                             |
| Amane 依赖 libc  | `libpython3.14.so` + glibc（`libc` / `libm` / `ld-linux-x86-64`）      | `ldd`                                                                     |
| Amane 鉴权       | 全部 `/api/*` 需 token，支持 `Authorization: Bearer`                       | [api.md](file:///d:/Trae/mhm-amane/amane-repo/docs/dev/api.md)            |
| Amane 前端       | React SPA，请求写死 `/api`，WS 走 `/api/ws` → 不能直接挂子路径                      | [main.tsx](file:///d:/Trae/mhm-amane/amane-repo/web/src/main.tsx#L45-L55) |

## 3. 三个硬卡点

| # | 卡点                                          | 后果               | 解决代价                                          |
| - | ------------------------------------------- | ---------------- | --------------------------------------------- |
| 1 | **libc 不兼容**：宿主 Alpine=musl，Amane 运行时=glibc | Python 与依赖直接无法启动 | 宿主运行镜像换 Debian（或 Amane 提供 musl 运行时，不现实）       |
| 2 | **体积超限**：约 270–300MB vs 128MB               | 安装被拒             | 宿主导出限额提到 512MB（zip 预计 110–150MB，上传上限提到 600MB） |
| 3 | **无 WebSocket**                             | 实时日志/进度不可用       | UI 轮询可绕开；若要实时再让宿主补反代                          |

另有 3 个非阻塞但必须说明的点：

* **媒体目录**：模块不能声明 volume。整理、路径设置依赖 `/media` 挂载，需用户在 compose 加一行；不挂载时只能"囤元数据"。

* **升级回滚**：宿主回滚只还原二进制，Amane 的 SQLite 迁移不可逆 → 升级前必须备份 DB，回滚需人工恢复。

* **首次启动**：Amane 迁移可能超 5 秒 → 健康检查由 watch 自己应答，Amane 就绪状态另放在 `/status`。

## 4. 推荐架构（简版）

```
浏览器 → /api/m/amane/ui/*（模块前端，纯 ESM）
       → /api/m/amane/status | /api/*（宿主反代，注入 X-Mica-Token）
宿主 → amane-watch（sidecar）
       → python -m amane.server（127.0.0.1，watch 注入 Bearer）
数据 → /data/modules/amane/{amane.db, resources, plugins, backups}
```

* `amane-watch` 用 Go 标准库单二进制：进程管理、DB 备份、`/health`、`/status`、`/api/*` 反代。

* 前端零构建，按宿主规范（`.mh-amane` 前缀、`--mh-*` 令牌、卡片 1.5 秒）。

* 不嵌 Amane 原生 SPA，避免给上游打 base path 补丁与维护 fork。

## 5. 首版功能范围（已确认）

| 功能   | 说明                     | 依赖                      |
| ---- | ---------------------- | ----------------------- |
| 刮削   | 输入番号/批量番号提交，查看结果       | Amane `POST /api/tasks` |
| 整理   | 按路径模板写出 NFO/图片/字幕      | 必须挂载 `/media`，需先建媒体库与模板 |
| 翻译   | LLM 开关、模型与语言配置         | Amane config + 用户自备模型   |
| 路径设置 | safe\_dirs、媒体库根目录、路径模板 | 环境变量 + 库配置              |
| 刮削记录 | 任务列表、状态、进度、取消/重试、任务日志  | 轮询 `GET /api/tasks`     |

不做：演员深度治理、AI 对话、插件页、内置播放、（媒体库扫描可选）。

## 6. 宿主改动需求（可直接转给 Mica Home 方）

> 标题：Amane 模块所需的宿主支持

1. **运行镜像改为 glibc**
   现状：运行层 `alpine:3.19`（musl）。模块 sidecar 为原生 Python 运行时（glibc 轮子），在 musl 下报 `Error loading shared library libpython3.14.so.1.0`。
   请求：运行层改为 `debian:bookworm-slim`（补 `ca-certificates`、`tzdata`）；构建层保持 `golang:alpine` + `CGO_ENABLED=0`，产物仍为静态二进制。镜像体积预计 +60MB。

2. **放宽模块包限额**
   现状：`maxArchiveBytes=160MB`、`maxSidecarBytes=128MB`、`maxArchiveEntryBytes=128MB`。
   请求：分别调整为 600MB / 512MB / 512MB；安装页与模块文档同步更新，并在安装页展示解压体积与磁盘余量。

3. **（可选，P1）反代支持 WebSocket**
   现状：`sidecarProxy` 把 `Upgrade` 当 hop-by-hop 头丢弃，WebSocket 无法透传。
   请求：改用 `httputil.ReverseProxy`（自定义 `DialContext` 支持 Unix socket），或对 `Upgrade` 走 `Hijack`。
   影响：不补也能跑（模块前端用轮询），但实时日志、后续嵌原生 UI 需要。

4. **文档：媒体目录需用户自行挂载**
   模块无法声明 volume。请在使用说明中明确：启用"整理/媒体库"时需在 `docker-compose.yml` 增加 `- /path/media:/media`，并在模块里把 `AMANE_SAFE_DIRS` 设为 `/media`。

5. **（可选）安装失败的可读性**
   大包上传失败目前只有通用错误。请求返回具体原因（限额、磁盘、哈希不匹配），并在安装页给出上限数值。

## 7. 风险与备选

| 风险        | 影响         | 备选                                               |
| --------- | ---------- | ------------------------------------------------ |
| 宿主不改      | 方案不成立      | 轻量入口模块（卡片 + 新窗口打开独立 Amane，零 sidecar）             |
| 300MB 超限  | 安装失败       | 裁依赖（如 asyncpg 14MB）或首启时用 uv 下载运行时（依赖网络）          |
| 媒体目录未挂载   | 整理/路径功能不可用 | 明确"仅元数据模式"，把整理标为需 compose 改动                     |
| DB 迁移不可回滚 | 降级失败       | watch 强制备份 + 模块页提供恢复入口                           |
| 反爬/出网     | 刮削失败       | `network.proxy` 指向 mihomo `127.0.0.1:7890`，设置页开关 |

## 8. 下一步

* 若认可结论：先把第 6 节发对方；同时可先做"假 sidecar + 假数据前端"在现有宿主上联调 UI 与挂载契约。

* 宿主改动通过后，再进入实施计划（Go watch、纯 ESM 前端 5 个页面、打包脚本、验收清单）。

