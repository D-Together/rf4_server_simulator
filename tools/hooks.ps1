# 启用/检查仓库内钩子（不依赖 GNU Make）。
#
# 用法：
#   powershell -File tools\hooks.ps1            # 启用 .githooks
#   powershell -File tools\hooks.ps1 -Check     # 启用后跑一次 pre-commit 验证
#   powershell -File tools\hooks.ps1 -Uninstall # 取消钩子（恢复 git 默认 .git/hooks）
#
# 等价于 `git config core.hooksPath .githooks`；`make hooks` 做的是同一件事。

[CmdletBinding()]
param(
    [switch]$Check,
    [switch]$Uninstall
)

$ErrorActionPreference = 'Stop'

$root = (& git rev-parse --show-toplevel 2>$null)
if (-not $root) {
    Write-Error "当前目录不在 git 仓库内。"
    exit 2
}
Set-Location $root

if ($Uninstall) {
    & git config --unset core.hooksPath 2>$null
    if ($LASTEXITCODE -eq 0) {
        Write-Host "已取消 core.hooksPath，钩子回到 .git/hooks。" -ForegroundColor Yellow
    } else {
        Write-Host "本来就没有配置 core.hooksPath。" -ForegroundColor Yellow
    }
    exit 0
}

$hooksDir = Join-Path $root '.githooks'
if (-not (Test-Path (Join-Path $hooksDir 'pre-commit'))) {
    Write-Error "找不到 .githooks/pre-commit，仓库不完整。"
    exit 2
}

& git config core.hooksPath .githooks
if ($LASTEXITCODE -ne 0) {
    Write-Error "git config core.hooksPath 失败。"
    exit 2
}

Write-Host "hooks -> .githooks" -ForegroundColor Green
Write-Host "  pre-commit : go run ./tools/doccheck（文档索引 / spec 状态 / 死链）"
Write-Host "  pre-push   : go build ./... + go vet ./..."

if ($Check) {
    Write-Host "验证：git hook run pre-commit" -ForegroundColor Cyan
    & git hook run pre-commit
    if ($LASTEXITCODE -ne 0) {
        Write-Error "pre-commit 验证失败（上方为 doccheck 输出）。"
        exit $LASTEXITCODE
    }
    Write-Host "pre-commit 通过。" -ForegroundColor Green
}
