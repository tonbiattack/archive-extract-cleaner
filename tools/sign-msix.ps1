#!/usr/bin/env pwsh
# MSIX の署名と検証を行う。
#
# パスワードはコマンドラインに載せない。環境変数から読む。
# コマンドライン引数はプロセス一覧やログに残るため。
#
# ローカル検証用の自己署名証明書は、その端末に証明書を信頼させないと
# インストールできない。Store 提出用の署名は Microsoft が行うため、
# ここで作る署名は配布物には使わない。
#
# 使い方:
#   $env:MSIX_SIGN_PASSWORD = "<パスワード>"
#   pwsh tools/sign-msix.ps1 -Package build/msix/ZipSweep.msix -Pfx cert.pfx

[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Package,
    [Parameter(Mandatory = $true)][string]$Pfx,
    # パスワードを保持する環境変数名。
    [string]$PasswordEnv = "MSIX_SIGN_PASSWORD"
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path -LiteralPath $Package)) {
    throw "パッケージが見つかりません: $Package"
}
if (-not (Test-Path -LiteralPath $Pfx)) {
    throw "証明書が見つかりません: $Pfx"
}
$password = [Environment]::GetEnvironmentVariable($PasswordEnv)
if ([string]::IsNullOrEmpty($password)) {
    throw "環境変数 $PasswordEnv に署名用パスワードを設定してください。"
}

$signtool = (Get-Command signtool.exe -ErrorAction SilentlyContinue).Source
if (-not $signtool) {
    $signtool = Get-ChildItem "C:\Program Files (x86)\Windows Kits\10\bin" -Directory -ErrorAction SilentlyContinue |
        Sort-Object Name -Descending |
        ForEach-Object { Join-Path $_.FullName "x64\signtool.exe" } |
        Where-Object { Test-Path -LiteralPath $_ } |
        Select-Object -First 1
}
if (-not $signtool) {
    throw "signtool.exe が見つかりません。Windows SDK をインストールしてください。"
}

& $signtool sign /fd SHA256 /f $Pfx /p $password /td SHA256 /tr http://timestamp.digicert.com $Package
if ($LASTEXITCODE -ne 0) {
    throw "署名に失敗しました（exit $LASTEXITCODE）"
}
& $signtool verify /pa /v $Package
if ($LASTEXITCODE -ne 0) {
    throw "署名の検証に失敗しました（exit $LASTEXITCODE）"
}
$signature = Get-AuthenticodeSignature -LiteralPath $Package
Write-Host "signature status: $($signature.Status)"
Write-Host "signer: $($signature.SignerCertificate.Subject)"
