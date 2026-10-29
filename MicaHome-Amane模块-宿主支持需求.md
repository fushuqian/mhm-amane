# Amane 模块对接状态（更新稿）

> 致：Mica Home 维护者
> 更新时间：2026-10-30
> 状态：**原提的三项宿主改动已全部实现在 f29bbc3，模块侧已对齐。**

## 1. 结论

原需求文档中要求的 3 项支持均已落地：

| 原需求 | 现状 |
|---|---|
| 运行镜像需 glibc | 运行层已改为 `debian:bookworm-slim`；纯 Go 静态 sidecar 同样适用 |
| 放宽模块包限额 | sidecar 512MB、单文件 512MB、上传 256MB、文件数 30000 |
| 反代支持 WebSocket | 已改用 `ReverseProxy`，支持 WebSocket / HTTP Upgrade 与流式响应（`FlushInterval=-1`） |

另外新增的 `sidecar.healthTimeoutSec`（1–120 秒）也已使用。

## 2. 模块侧已做的对齐

- sidecar 仍为静态编译的 `linux/amd64` 单二进制（约 18MB，模块包约 7.8MB），无需 Python 运行时。
- 读取 `MICA_SIDECAR_ADDR`（`tcp://` 与 `unix://` 均支持），监听回环或 Unix socket。
- `healthPath=/health`，并声明 `healthTimeoutSec=10`。
- 模块通道校验宿主注入的 `X-Mica-Token`：无 token 或 token 不匹配返回 401。
- 不占用 `info` / `ui` / `__status` / `__logs` / `__host` 等宿主保留路径。
- 数据只写 `MICA_MODULE_DATA_DIR`，日志走宿主捕获的 stdout/stderr。

## 3. 仍需部署侧配合的一项

模块不能声明 volume。要把「整理 / 路径模板」用起来，需要在 Mica Home 的 compose 里挂载媒体目录：

```yaml
services:
  mica-home:
    volumes:
      - /data:/data
      - /vol1/1000/media:/media   # 按实际路径
```

模块 manifest 已把 `AMANE_MEDIA_DIR` 固定为 `/media`，挂载后无需额外配置。未挂载时仅「元数据模式」可用，刮削与浏览不受影响。

## 4. 后续计划（模块方）

- 继续移植 Amane 其余站点。
- 接入内置 Chrome headless（宿主已是 glibc 且 sidecar 上限 512MB，条件已满足）；届时模块包会明显增大，仍在上限内。
- 如需要，可利用新的 WebSocket 反代把任务进度从轮询改为实时推送。
