# 关键配置与路径

- 数据目录：`./data`（桌面/容器由 `AMANE_DATA_DIR` 或 `MICA_MODULE_DATA_DIR` 覆盖）
  - `config.json`：站点顺序、代理、整理参数
  - `amane.db`：元数据、文件索引、任务
- 整理根目录：`-media` 或 `AMANE_MEDIA_DIR`；Mica Home 模块 manifest 里固定 `/media`
- 监听：`AMANE_ADDR` 或 `-addr`；Mica Home 注入 `MICA_SIDECAR_ADDR`（`tcp://` 或 `unix://`）
- 本仓库无密钥。站点 cookie 位于 `config.json` 的 `site_settings`，不要提交到公共仓库。
