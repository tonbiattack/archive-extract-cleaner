#!/usr/bin/env pwsh
# 提出用 MSIX を組み立てる。
#
# 正式な Identity Name / Publisher は Partner Center の名前予約後に確定する。
# このスクリプトは環境変数から受け取り、値が無ければ失敗する。
# 仮の識別子で作った成果物は Store に提出できないため、既定値を持たせない。
#
# 使い方:
#   $env:MSIX_IDENTITY_NAME = "12345Publisher.ZipSweep"
#   $env:MSIX_PUBLISHER     = "CN=ABCDEF12-..."
#   $env:MSIX_VERSION       = "0.1.0.0"
#   pwsh tools/build-msix.ps1 -Exe build/bin/archive-extract-cleaner.exe -Out build/msix

[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Exe,
    [Parameter(Mandatory = $true)][string]$Out,
    [string]$Assets = "build/store-assets"
)

$ErrorActionPreference = "Stop"

function Require-Env([string]$Name) {
    $value = [Environment]::GetEnvironmentVariable($Name)
    if ([string]::IsNullOrWhiteSpace($value)) {
        throw "環境変数 $Name が未設定です。Partner Center で確定した値を設定してください。"
    }
    return $value
}

$identityName = Require-Env "MSIX_IDENTITY_NAME"
$publisher = Require-Env "MSIX_PUBLISHER"
$version = Require-Env "MSIX_VERSION"

if ($version -notmatch '^\d+\.\d+\.\d+\.\d+$') {
    throw "MSIX_VERSION は 4 桁の形式（例 0.1.0.0）で指定してください: $version"
}

if (-not (Test-Path -LiteralPath $Exe)) {
    throw "実行ファイルが見つかりません: $Exe"
}
if (-not (Test-Path -LiteralPath $Assets)) {
    throw "Store 用素材が見つかりません: $Assets（python tools/make_icon.py で生成してください）"
}

# MakeAppx は Windows SDK に含まれる。PATH に無ければ既定の場所を探す。
$makeappx = (Get-Command makeappx.exe -ErrorAction SilentlyContinue).Source
if (-not $makeappx) {
    $candidates = Get-ChildItem "C:\Program Files (x86)\Windows Kits\10\bin" -Directory -ErrorAction SilentlyContinue |
        Sort-Object Name -Descending |
        ForEach-Object { Join-Path $_.FullName "x64\makeappx.exe" } |
        Where-Object { Test-Path -LiteralPath $_ }
    $makeappx = $candidates | Select-Object -First 1
}
if (-not $makeappx) {
    throw "makeappx.exe が見つかりません。Windows SDK をインストールしてください。"
}

$stage = Join-Path ([IO.Path]::GetTempPath()) ("zipsweep-msix-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $stage -Force | Out-Null
New-Item -ItemType Directory -Path (Join-Path $stage "Assets") -Force | Out-Null

try {
    Copy-Item -LiteralPath $Exe -Destination (Join-Path $stage "archive-extract-cleaner.exe")
    Copy-Item -Path (Join-Path $Assets "*.png") -Destination (Join-Path $stage "Assets")

    $manifest = @"
<?xml version="1.0" encoding="utf-8"?>
<Package
  xmlns="http://schemas.microsoft.com/appx/manifest/foundation/windows10"
  xmlns:uap="http://schemas.microsoft.com/appx/manifest/uap/windows10"
  xmlns:uap10="http://schemas.microsoft.com/appx/manifest/uap/windows10/10"
  xmlns:rescap="http://schemas.microsoft.com/appx/manifest/foundation/windows10/restrictedcapabilities"
  IgnorableNamespaces="uap uap10 rescap">
  <Identity Name="$identityName" Publisher="$publisher" Version="$version" ProcessorArchitecture="x64" />
  <Properties>
    <DisplayName>ZipSweep</DisplayName>
    <PublisherDisplayName>ZipSweep</PublisherDisplayName>
    <Description>ZIP などのアーカイブと解凍済みフォルダを照合し、完全一致したフォルダをごみ箱へ移動するツール</Description>
    <Logo>Assets\StoreLogo.png</Logo>
  </Properties>
  <Dependencies>
    <TargetDeviceFamily Name="Windows.Desktop" MinVersion="10.0.19041.0" MaxVersionTested="10.0.26100.0" />
  </Dependencies>
  <Resources>
    <Resource Language="ja-JP" />
    <Resource Language="en-US" />
  </Resources>
  <Applications>
    <Application Id="App" Executable="archive-extract-cleaner.exe"
      uap10:RuntimeBehavior="packagedClassicApp" uap10:TrustLevel="mediumIL">
      <uap:VisualElements
        DisplayName="ZipSweep"
        Description="ZIP などのアーカイブと解凍済みフォルダを照合し、完全一致したフォルダをごみ箱へ移動するツール"
        Square150x150Logo="Assets\Square150x150Logo.png"
        Square44x44Logo="Assets\Square44x44Logo.png"
        BackgroundColor="transparent" />
    </Application>
  </Applications>
  <Capabilities>
    <rescap:Capability Name="runFullTrust" />
  </Capabilities>
</Package>
"@
    $manifestPath = Join-Path $stage "AppxManifest.xml"
    [IO.File]::WriteAllText($manifestPath, $manifest, [Text.UTF8Encoding]::new($false))

    New-Item -ItemType Directory -Path $Out -Force | Out-Null
    $package = Join-Path $Out "ZipSweep.msix"
    & $makeappx pack /d $stage /p $package /o
    if ($LASTEXITCODE -ne 0) {
        throw "MakeAppx が失敗しました（exit $LASTEXITCODE）"
    }
    Write-Host "created $package"
    Get-Item -LiteralPath $package | Select-Object FullName, Length
}
finally {
    Remove-Item -LiteralPath $stage -Recurse -Force -ErrorAction SilentlyContinue
}
