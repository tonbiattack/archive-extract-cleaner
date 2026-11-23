#!/usr/bin/env pwsh
# MSIX の署名と検証を行う。
#
# ローカル検証用の自己署名証明書は、その端末に証明書を信頼させないと
# インストールできない。Store 提出用の署名は Microsoft が行うため、
# ここで作る署名は配布物には使わない。
#
# 使い方:
#   pwsh tools/sign-msix.ps1 -Package build/msix/ZipSweep.msix -Pfx cert.pfx -Password (Read-Host -AsSecureString)

[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Package,
    [Parameter(Mandatory = $true)][string]$Pfx,
    [Parameter(Mandatory = $true)][securestring]$Password
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path -LiteralPath $Package)) {
    throw "パッケージが見つかりません: $Package"
}
if (-not (Test-Path -LiteralPath $Pfx)) {
    throw "証明書が見つかりません: $Pfx"
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

$plain = [Runtime.InteropServices.Marshal]::PtrToStringAuto(
    [Runtime.InteropServices.Marshal]::SecureStringToBSTR($Password))

try {
    & $signtool sign /fd SHA256 /f $Pfx /p $plain /td SHA256 /tr http://timestamp.digicert.com $Package
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
}
finally {
    if ($plain) {
        $plain = $null
    }
}
