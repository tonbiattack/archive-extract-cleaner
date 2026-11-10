# Synced from tonbiattack/multi-drive-copy MultiDriveCopy.Core.ps1 Read-SourceListFile.
# Used only for compatibility tests of the -SourceListFile manifest schema.
function Read-SourceListFile {
    param([string]$Path)

    if (-not $Path) { throw 'ソースリストファイルが指定されていません。' }
    $manifestPath = [string]$Path
    if (-not [IO.Path]::IsPathRooted($manifestPath)) {
        $manifestPath = [IO.Path]::GetFullPath((Join-Path (Get-Location).Path $manifestPath))
    }
    if (-not [IO.File]::Exists($manifestPath)) {
        throw 'ソースリストファイルが見つかりません: ' + $manifestPath
    }

    try {
        $json = [IO.File]::ReadAllText($manifestPath, [Text.Encoding]::UTF8)
        $manifest = $json | ConvertFrom-Json
    } catch {
        throw 'ソースリストをUTF-8 JSONとして読み込めません: ' + $_.Exception.Message
    }

    if ($null -eq $manifest -or -not $manifest.PSObject.Properties['sources']) {
        throw 'ソースリストに sources 配列がありません。'
    }
    $raw = $manifest.sources
    if ($null -eq $raw) { throw 'sources が空です。' }
    if ($raw -isnot [System.Array]) {
        throw 'sources は文字列の配列で指定してください。'
    }

    $paths = New-Object System.Collections.ArrayList
    $seen = @{}
    foreach ($item in $raw) {
        if ($item -isnot [string]) {
            throw 'sources には文字列のパスだけを指定してください。'
        }
        $value = ([string]$item).Trim().Replace('/', '\')
        if (-not $value) { throw 'sources に空のパスがあります。' }
        if ($value -notmatch '^([A-Za-z]:\\|\\\\)') {
            throw 'sources にはフルパスを指定してください: ' + $value
        }
        try {
            $full = [IO.Path]::GetFullPath($value)
        } catch {
            throw 'パスが不正です: ' + $value
        }
        if ($full.Length -gt 3) { $full = $full.TrimEnd('\') }
        if (-not (Test-Path -LiteralPath $full)) {
            throw 'コピー元が見つかりません: ' + $full
        }
        $key = $full.ToLowerInvariant()
        if (-not $seen.ContainsKey($key)) {
            $seen[$key] = $true
            [void]$paths.Add($full)
        }
    }
    if ($paths.Count -eq 0) { throw 'sources が空です。' }
    return @($paths)
}
