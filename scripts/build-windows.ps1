# 在 Windows 上构建 api-doc-go-viva。
#
#   powershell -ExecutionPolicy Bypass -File .\scripts\build-windows.ps1
#   powershell -ExecutionPolicy Bypass -File .\scripts\build-windows.ps1 -Arch arm64
#   powershell -ExecutionPolicy Bypass -File .\scripts\build-windows.ps1 -Console   # 保留控制台看日志
#
# 产物默认 dist\api-doc-viva-windows-<arch>.exe。
# Viva 的 Windows 后端是纯 Go（purego 动态调用），CGO_ENABLED=0 即可，无需 MinGW。
[CmdletBinding()]
param(
    [ValidateSet('amd64', 'arm64')]
    [string]$Arch = 'amd64',

    # 默认用 windowsgui 子系统，双击不弹黑框；调试时加 -Console。
    [switch]$Console,

    [string]$OutDir = 'dist'
)

$ErrorActionPreference = 'Stop'

$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

$go = Get-Command go -ErrorAction SilentlyContinue
if (-not $go) {
    throw '找不到 go：请安装 Go 1.26+（winget install GoLang.Go）后重开终端'
}

$outDirPath = Join-Path $root $OutDir
New-Item -ItemType Directory -Force -Path $outDirPath | Out-Null
$out = Join-Path $outDirPath "api-doc-viva-windows-$Arch.exe"

$ldflags = '-s -w'
if (-not $Console) {
    $ldflags = "$ldflags -H windowsgui"
}

$env:CGO_ENABLED = '0'
$env:GOOS = 'windows'
$env:GOARCH = $Arch
if (-not $env:GOWORK) {
    $env:GOWORK = 'off'
}

Write-Host "构建 $out（GOOS=windows GOARCH=$Arch CGO_ENABLED=0）"
& go build -trimpath -buildvcs=false -ldflags $ldflags -o $out .
if ($LASTEXITCODE -ne 0) {
    throw "go build 失败（退出码 $LASTEXITCODE）"
}

$exe = Get-Item $out
Write-Host ('完成：{0}（{1:N1} MB）' -f $exe.FullName, ($exe.Length / 1MB))
