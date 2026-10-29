# Amane 模块界面与自动流程调整方案

## 1. 目标

按老板的 5 条意见重整界面，并把「手动提交」改为「定时自动扫描 → 刮削 → 整理」：

1. 片库改名为「新片速递」，按首次入库时间倒序。
2. 「刮削记录」改名为「任务」，只显示进行中的任务，去掉手动输入项。
3. 设置页改成分组折叠，点开才展开。
4. 目录管理支持 1 个片库路径 + 多个下载库路径；每个下载路径一个横向单选的整理方式；全局一套文件名模板。
5. 写出 NFO / 写出图片默认开启。

## 2. 现状分析

| 位置 | 现状 | 影响 |
|---|---|---|
| [config.go](file:///d:/Trae/mhm-amane/amane-lite/internal/config/config.go#L16-L34) | 单套 `Library{root,mode,dir_template,file_template}`，无下载目录概念 | 无法表达「片库 1 个 + 下载库多个 + 每个下载目录独立整理方式」 |
| [organize.go](file:///d:/Trae/mhm-amane/amane-lite/internal/organize/organize.go#L28-L82) | `Execute` 接收整份 `config.Library`，所有文件用同一模式 | 无法按来源目录区分 PT / 迅雷 |
| [worker.go](file:///d:/Trae/mhm-amane/amane-lite/internal/worker/worker.go#L150-L182) | 只有 `scan` / `scrape` / `organize` 三类任务，无定时调度 | 没有自动链路，必须手动提交 |
| [store.go](file:///d:/Trae/mhm-amane/amane-lite/internal/store/store.go#L166-L200) | 列表按 `updated_at DESC` | 重刮老片会把它顶到「新片速递」顶部 |
| [app.js](file:///d:/Trae/mhm-amane/amane-lite/internal/api/web/app.js#L86-L121) | 三个平铺标签；设置页一次性铺开全部字段；任务页含提交表单 | 与目标交互不符 |
| 任务列表 | `ListTasks` 返回全部历史 | 无法只显示进行中 |

## 3. 配置结构（v2）

`internal/config/config.go` 改为：

```go
type DownloadDir struct {
    Path string `json:"path"`
    Mode string `json:"mode"` // copy / move（只保留两种；跨宿主机与 Docker 的硬链接/符号链接不可用）
}

type Library struct {
    Root         string `json:"root"`          // 唯一的片库路径
    VideoTemplate string `json:"video_template"` // 视频路径模板（相对库根，原版语法）
    WriteNFO     bool   `json:"write_nfo"`     // 默认 true
    WriteImages  bool   `json:"write_images"`  // 默认 true
}

type Automation struct {
    Enabled         bool `json:"enabled"`          // 默认 true
    IntervalMinutes int  `json:"interval_minutes"` // 默认 30，范围 5–1440
    RunOnStart      bool `json:"run_on_start"`     // 默认 true
}

type Config struct {
    Addr             string
    UseMihomoProxy   bool // 网络分组只保留这一个开关；开启即走 http://127.0.0.1:7890
    Sites            []string
    SiteSettings     map[string]SiteSetting
    Library          Library
    DownloadDirs     []DownloadDir
    Automation       Automation
}
```

要点：

- 删除旧的 `MediaRoot`、`Library.Mode`、`DirTemplate`、`FileTemplate` 与自由填写的 `Proxy`；`UseMihomoProxy` 默认 `false`，开启时 `EffectiveProxy()` 返回 `http://127.0.0.1:7890`，关闭返回空串。
- `Normalize()`：`VideoTemplate` 为空 → 默认 `{actress}/{number}[-CD{cd?}][-{sub?}].{ext}`；`IntervalMinutes` 为 0 → 30。
- **模板只写相对片库根的部分，系统自动拼上片库路径**：例如模板 `{actress}/{number}[-CD{cd?}][-{sub?}].{ext}`，片库根 `/media`，最终落盘 `/media/三田サクラ/MIDV-123.mp4`。若用户误贴了以片库根开头的绝对路径（`/media/{actress}/...`），保存时自动去掉这段前缀并按相对模板存储，不报错。
- 旧配置升级：`library.dir_template + library.file_template` 拼成 `dir_template/file_template.{ext}` 作为初始 `VideoTemplate`；旧 `hardlink` / `symlink` 一律回落为 `copy`（不删除源文件，最安全）。
- 校验：片库路径非空；下载目录路径非空、去重、不得与片库路径相同；`Mode` 只接受 `copy` / `move`；模板保存前做语法与占位符校验（未知占位符、非法映射键直接拒绝）。
- **允许根（SAFE_DIRS）**：由环境变量 `AMANE_SAFE_DIRS` 提供，默认 `/media,/downloads`。片库路径与下载目录都必须落在这些根之内；不在范围内的选择直接 422。宿主 compose 把这两个目录挂进来即可。

## 4. 后端改动

### 4.1 整理（`internal/organize/organize.go` + 新增 `template.go`）

- 不再依赖 `config.Library`，改为本包的 `Options{Root, Mode, VideoTemplate string; WriteNFO, WriteImages bool}`。
- 按原版「视频路径模板」计算目标路径（相对片库根，`/` 分隔层级），而不是目录模板 + 文件名模板两段。
- 支持原版语法：
  - 占位符：`{number}` `{prefix}` `{suffix}` `{title}` `{actor}` `{actors}` `{actress}` `{actresses}` `{studio}` `{publisher}` `{series}` `{year}` `{release}` `{ext}` `{content_type}` `{cd?}` `{sub?}` `{mosaic?}` `{def?}` `{raw_name}` `{raw_dir}` `{video_dir}` `{video_name}` `{video_relpath}`；
  - 可选组 `[...]`（组内直接占位符全空则整组省略）、双层 `[[...]]`；
  - 值映射 `{name|原值=输出,原值=输出}`，空值可用 `{name|=缺省}`；
  - 缺字段输出 `Unknown`，`{title}` 为空时回退番号；`{actor}`/`{actress}` 分别取第一位；演员类截断到 200 字节；
  - `{ext}` 取自源文件（模板写 `.mp4` 但源文件是 `.mkv` 时仍输出 `.mkv`）。
- 分两阶段注入：先按元数据 + 文件相位算出 `{video_dir}` / `{video_name}` / `{video_relpath}`，再给附属文件（NFO/图片）用同一结果。
- 路径安全：渲染后逐段清理（非法字符、段尾 `.`/空格、空段省略），拒绝绝对路径与含 `..` 段，最终必须落在片库根内；`Library.Root` 由程序自动拼接，不出现在模板里。
- `placeVideo` 只保留 `copy`（复制后保留源文件）与 `move`（复制成功后删除源文件）；删除 hardlink / symlink 分支。
- 附属文件路径固定：视频同目录的 `poster.jpg`、`thumb.jpg`、`fanart.jpg`、`extrafanart/fanart-NN.jpg`，NFO 为 `{video_name}.nfo`。

### 4.2 Worker（`internal/worker/worker.go`）

新增任务类型 `sync`（扫描 → 刮削 → 整理 一条链路）：

1. 遍历 `DownloadDirs`（最长前缀匹配确认归属），识别视频、`ParsePath` 得到番号与文件相位，登记 `media_files`（新增 `cd` 列存分集，供 `{cd?}` 使用）。
2. 对每个新番号：
   - 已在 `metadata` 中存在 → 跳过刮削。
   - 不存在 → `aggregate.Run` 后 `SaveMetadata`（沿用现有站点顺序与 `use_cache` 语义）。
3. 对每个已登记文件：
   - 找到它所属的下载目录 → 取该目录的 `Mode`；
   - 取该文件对应条目的元数据 → `organize.Execute` 写入片库根目录。
4. 进度：`扫描 x/y` → `刮削 k/n` → `整理 k/n`。
5. 失败不中断后续文件；结果 JSON 汇总 `scanned/registered/scraped/organized/errors`。

定时调度（`Worker.Start` 内新增独立 ticker，每 60 秒判断）：

- `Automation.Enabled` 为真；
- 距上次入队 ≥ `IntervalMinutes`；
- 当前没有 `queued` / `running` 的 `sync` 任务（`store.HasActive("sync")`）；
- 满足则 `CreateTask("sync", SyncPayload{})`。
- 进程启动时若 `RunOnStart` 为真且没有活跃 `sync`，立即入队一次。

保留 `scrape` / `organize` / `scan` 三类任务供 API 与详情页使用，界面不再暴露手动入口。

### 4.3 Store（`internal/store/store.go`）

- `ListMetadata` 排序改为 `created_at DESC`（首次入库时间；重刮不改变 `created_at`，已有 `ON CONFLICT` 只更新 `updated_at`）。
- 新增 `ListActiveTasks(limit)`：`status IN ('queued','running')`。
- 新增 `HasActive(kind)`：用于调度去重。

### 4.4 API（`internal/api/server.go`）

- `GET /api/tasks?active=1` 返回进行中任务；默认仍返回全部（供内部排查）。
- `GET /api/fs?path=`：目录浏览。返回 `{path, parent, roots, entries:[{name,path,is_dir}]}`；只能浏览允许根（`AMANE_SAFE_DIRS`）之内的路径，越界返回 403；`path` 为空时列出各允许根。
- `POST /api/sites/check`：对每个已注册站点探测 `Profile().BaseURL`，并发执行、单站 10 秒超时；返回 `{items:[{name,status,detail}]}`，`status` 取 `ok | blocked | unreachable | timeout`。使用当前客户端（即当前代理开关状态）。
- `PATCH /api/config` 按 v2 结构校验并写回；失败保持原配置。写回成功后调用 `client.SetProxy(cfg.EffectiveProxy())`，启动时也用同一取值。
- 版本号升到 `0.2.0`。

### 4.5 模块清单与部署

- `module/manifest.json` 的 `sidecar.env` 增加：`AMANE_MEDIA_DIR=/media`、`AMANE_DOWNLOAD_DIR=/downloads`、`AMANE_SAFE_DIRS=/media,/downloads`。
- Mica Home 的 compose 需挂载：

```yaml
volumes:
  - /data:/data
  - /vol1/1000/media:/media
  - /vol1/1000/downloads:/downloads
```

- README 与沟通稿同步补这两条挂载说明；未挂载时目录选择器只显示存在的根。

## 5. 前端改动（`internal/api/web/app.js` / `style.css`）

### 5.1 标签

`新片速递` / `任务` / `设置`（顺序不变，改名两处）。

- 新片速递：**只显示最新 10 条**（`created_at DESC`），去掉「加载更多」；搜索仍可用，但同样最多返回 10 条；空态文案改为「扫描下载库后，新入库的影片会出现在这里」。
- 任务：删除整个提交表单；只拉 `GET /api/tasks?active=1`，每 2 秒刷新；每行显示类型（扫描 / 刮削 / 整理）、进度、状态；空态「暂无进行中的任务」。切走或卸载时清掉定时器。
- 详情页：保留「重新刮削 / 整理」两个维护按钮。

### 5.2 设置分组（折叠）

用自定义 header + panel（点击展开，同一时间只展开一组）：

| 分组 | 字段 |
|---|---|
| 常规设置 | 自动任务开关（默认开）、执行间隔（分钟，默认 30）、启动时执行一次 |
| 网络 | 一个开关「使用 mihomo 代理（127.0.0.1:7890）」；提示需先在 Mica Home 安装并启用 mihomo 模块 |
| 刮削站点 | 左侧一排方块，每个方块代表一个站点（下注站点名）；默认灰色。右侧「测试连通性」按钮：点击后立即 `POST /api/sites/check`，测试中显示脉冲态，返回后绿色=通、红色=不通，悬停显示失败原因 |
| 目录管理 | 片库路径（输入框 + 「浏览」按钮）、下载库路径列表（每条：输入框 + 「浏览」+ 删除），「添加下载目录」按钮。浏览打开文件浏览器：只能浏览 `/media`、`/downloads` 等允许根，面包屑 + 目录列表 + 上级 + 选择当前目录 |
| 整理方式 | 每个下载路径一行，横向单选：复制 / 移动；附一行说明「PT 做种用复制（源文件保留），迅雷等临时下载可移动」 |
| 视频路径模板 | 一条模板输入（默认 `{actress}/{number}[-CD{cd?}][-{sub?}].{ext}`）；**只写相对片库根的部分，系统自动补上片库路径**；下方列出可用占位符徽章与语法提示；写出 NFO、写出图片两个开关（默认勾选） |

占位符区域（参考原版）：

- 徽章分两组：影片字段 `{number}` `{prefix}` `{suffix}` `{title}` `{actor}` `{actors}` `{actress}` `{actresses}` `{studio}` `{publisher}` `{series}` `{year}` `{release}` `{content_type}`；文件相位 `{ext}` `{cd?}` `{sub?}` `{mosaic?}` `{def?}` `{raw_name}` `{raw_dir}` `{video_dir}` `{video_name}` `{video_relpath}`。
- 悬停徽章显示说明与示例（如 `{cd?}` → `1 / 2 / 空`，`{mosaic?}` → `censored / uncensored / cracked / leaked / 空`）。
- 底部一行语法提示：可选组 `[...]`、双层 `[[...]]`、值映射 `{name|原值=输出}`；`{video_dir}` `{video_name}` `{video_relpath}` 仅在后段取值时可用。

- 底部一个「保存设置」按钮，提交完整 v2 配置。
- 目录列表为空时显示引导：至少添加一个下载目录，自动任务才会工作。
- 表单渲染时对 `download_dirs` 做增删改的本地副本，保存时一次性提交。

### 5.3 样式

- 折叠组：`.mh-amane-group`（header 按钮 + chevron，panel 展开）。
- 横向单选：`.mh-amane-radio-row`（label + input[type=radio]，flex wrap，窄屏两列）。
- 目录行：`.mh-amane-path-row`（路径输入 + 删除按钮）。
- 站点方块：`.mh-amane-site-grid`（flex wrap）、`.mh-amane-site`（方块 + 站点名），状态类 `.s-unknown`（灰）/`.s-testing`（脉冲）/`.s-ok`（绿）/`.s-fail`（红）；颜色用 `--mh-accent` 与固定红，不新增全局变量。
- 占位符徽章：`.mh-amane-chip-row` + `.mh-amane-chip`（等宽字体、边框、悬停浮层说明）；窄屏自动换行。
- 文件浏览器：`.mh-amane-picker`（遮罩 + 面板）、`.mh-amane-crumbs`（面包屑）、`.mh-amane-dirlist`（目录行，点击进入）、底部「选择此目录 / 取消」；窄屏占满宽度。

## 6. 默认值与兼容

- 新装：`Library.Root=/media`；若 `/downloads` 存在，`DownloadDirs` 初始化为 `[{path:"/downloads", mode:"copy"}]`，否则为空；自动任务开启、30 分钟、启动执行一次；`write_nfo/write_images=true`。
- 老配置升级：`library.root` 保留；`library.mode` 丢弃；`download_dirs` 为空时自动任务不会做任何事（UI 提示先加下载目录），避免误把片库目录当下载目录。
- 校验片库路径与下载目录不同（相同直接 422/保存失败），避免原地整理。

## 7. 验证

1. `gofmt -w cmd internal && go build ./... && go vet ./... && go test ./...`。
2. 新增单测：
   - `config.Normalize` / 校验（空模板回落、非法 Mode、下载目录与片库相同）。
   - `store.ListMetadata` 按 `created_at` 倒序（同一批数据重刮后顺序不变）。
   - `store.ListActiveTasks` 只返回 queued/running。
   - 模板渲染（表驱动，用例取自原版文档）：`{number}[-CD{cd?}][-{sub?}].{ext}` 的四组输入；`[[{def?}]]` 双层组；值映射 `{mosaic?|censored=有码,uncensored=无码}`；未知占位符 → `Unknown`；含 `..` 的模板被拒绝；以片库根开头的绝对模板会被去掉前缀后按相对模板存储。
   - `/api/fs`：允许根内可列目录、越界路径返回 403、`path` 为空返回 roots。
3. 打包并升级安装到本机 Mica Home（0.2.0），确认权限确认 → 启用 → running。
4. 造数据：临时下载目录放 `MIDV-123.mp4`，配置该目录（mode=copy），`POST /api/tasks kind=sync`；确认任务结束、片库根目录出现视频/NFO/图片，`新片速递` 出现条目。
5. 浏览器验收：三个标签、新片速递最多 10 条且无加载更多、任务页无输入框、设置六组可折叠、横向单选可切换、站点方块测试后变绿/变红、保存后配置回读一致、深浅色正常。

## 8. 决策记录

- 定时自动执行，不保留手动刮削入口；详情页维护按钮保留。
- 片库只有一个；下载目录可多个，每个目录独立整理方式。
- 整理方式只保留「复制 / 移动」：PT 做种用复制（源文件保留），迅雷等临时下载用移动；硬链接与符号链接在宿主机 ↔ Docker 场景不可用，不实现。
- 网络分组只保留「使用 mihomo 代理」一个开关，地址固定 `http://127.0.0.1:7890`；不再暴露站点顺序与自由代理输入。
- 刮削站点单独一组：方块表示各站点连通性，点按钮即时测试（走当前代理设置）。
- 文件名模板并入「视频路径模板」：沿用原版一套模板（`/` 分层），支持可选组、值映射与原版占位符；**模板只写相对部分，片库根由程序自动补上**，不再要求用户手写 `/media/` 前缀；界面下方列出徽章与示例。
- 路径选择统一走文件浏览器：只允许浏览 `AMANE_SAFE_DIRS`（默认 `/media`、`/downloads`）；片库默认 `/media`，下载库默认 `/downloads`，由 Mica Home compose 挂载。
- 「新片速递」按首次入库时间倒序，固定最多 10 条（搜索同样 10 条，不分页）。
- NFO 与图片默认写出，可在设置里关闭。
