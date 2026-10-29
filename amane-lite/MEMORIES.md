# 经验记录

- Amane 的番号解析用了 Python 的环视（`(?<!\d)` / `(?![.-]\d)`），Go 的 RE2 不支持；必须改成「先匹配再手工校验相邻字符」，否则规则会静默放宽。
- Amane 的 `identify` 会先 `upper()` 再匹配，所以解析结果番号是大写；移植时测试期望值要按大写写。
- `removeEscapeStrings` 去掉分辨率标记后，`1080p` 不会参与番号；`_prepare` 的三个阶段（escaped / dated / catalog）不能合并，国产 MD 与欧美日期号依赖不同阶段。
- tls-client v1.x 用的是 `github.com/bogdanfinn/fhttp`，不是标准 `net/http`；请求与响应类型都要用 fhttp。
- `profiles.ClientProfile` 含不可比较字段，不能与零值比较；用指针 + nil 判断。
- Mica Home 的 sidecar 反代在 f29bbc3 起已支持 WebSocket / HTTP Upgrade 与流式响应（`FlushInterval=-1`）；需要实时推送时可直接用 WS，不必只靠轮询。
- Mica Home 的 `/api/m/<id>/ui/*` 只服务前端静态文件；业务请求走 `/api/m/<id>/<path>`，sidecar 收到的是去掉前缀后的路径。
- Mica Home 的 sidecar 上限在 f29bbc3 起为 512MB（单文件 512MB，文件数 30000，上传 256MB）；宿主也支持 `healthTimeoutSec`（1–120 秒，超时只标 degraded，不杀进程）。
- 宿主运行层在 f29bbc3 起改为 Debian slim（glibc），纯 Go 静态二进制照样能跑；这也让打包 Chrome headless 成为可能。
- 宿主安装模块后会注入 `MICA_SIDECAR_TOKEN`，sidecar 应在模块通道校验 `X-Mica-Token`；Go 版已加 TokenGuard（未设置该环境变量时放行，便于本地开发）。
- 配置升级要用「原始 JSON 里有没有新字段」判断，不能看结构体值：先 unmarshal 到带默认值的结构体后，缺省字段已变成默认值，旧值会被静默吞掉。
- 复制模式下源文件会一直留在下载库，自动任务每轮都会重复整理；目标已存在且大小一致时要直接跳过，图片也要「已存在就不下载」。
- 可选组 `[..]` 的省略规则是「组内直接占位符全空」，不是「渲染结果为空」：`[-CD{cd?}]` 在 CD 为空时必须连 `-CD` 一起去掉。
- 硬链接/符号链接跨宿主机与 Docker 挂载没有意义（不同文件系统、不同 inode），整理方式只保留复制与移动。
