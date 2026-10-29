# 构建 Mica Home 模块包（linux/amd64 sidecar + 前端）。
# 用法：powershell -ExecutionPolicy Bypass -File scripts/build-module.ps1
# 交叉编译、manifest 哈希回写与 zip 打包都在 Go 里做，避免 PowerShell 写 BOM。
$ErrorActionPreference = 'Stop'
Set-Location (Split-Path -Parent $PSScriptRoot)
go run ./cmd/pack -root .
