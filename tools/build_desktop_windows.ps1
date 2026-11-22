param([Parameter(Mandatory=$true)][string]$Output)
$ErrorActionPreference = 'Stop'
$repository = Split-Path -Parent $PSScriptRoot
$destination = [IO.Path]::GetFullPath($Output)
if ((Test-Path -LiteralPath $destination) -and (Get-ChildItem -LiteralPath $destination | Select-Object -First 1)) { throw 'Choose an empty package directory' }
New-Item -ItemType Directory -Force -Path (Join-Path $destination 'runtime/bin') | Out-Null
Push-Location -LiteralPath $repository
$priorCGO=$env:CGO_ENABLED
try {
    $env:CGO_ENABLED='0'
    go build -trimpath -o (Join-Path $destination 'opensync-cli.exe') ./cmd/opensync
    if ($LASTEXITCODE -ne 0) { throw 'CLI build failed' }
    Push-Location -LiteralPath (Join-Path $repository 'desktop')
    try {
        go run ./tools/winresources
        if ($LASTEXITCODE -ne 0) { throw 'Windows manifest build failed' }
        go build -trimpath -tags desktop,production -ldflags '-H windowsgui' -o (Join-Path $destination 'opensync.exe') .
        if ($LASTEXITCODE -ne 0) { throw 'Desktop build failed' }
        $notices=Join-Path $destination 'Third-party licenses'
        New-Item -ItemType Directory -Force -Path $notices | Out-Null
        $modules=@(go list -m -f '{{.Path}}|{{.Version}}|{{.Dir}}' all)
        if ($LASTEXITCODE -ne 0) { throw 'Dependency inventory failed' }
        $modules | Set-Content -LiteralPath (Join-Path $notices 'modules.txt')
        foreach ($line in $modules) {
            $parts=$line -split '\|'
            if ($parts.Length -lt 3 -or !$parts[2] -or $parts[0] -like 'opensync*') { continue }
            $name=$parts[0] -replace '[\\/:]','_'
            Get-ChildItem -LiteralPath $parts[2] -File | Where-Object Name -Match '^(LICENSE|COPYING|NOTICE)' | ForEach-Object { Copy-Item -LiteralPath $_.FullName -Destination (Join-Path $notices ($name+'-'+$_.Name)) }
        }
    } finally {
        $resource=[IO.Path]::GetFullPath((Join-Path $repository 'desktop/rsrc_windows_amd64.syso'))
        if ((Split-Path -Parent $resource) -ne (Join-Path $repository 'desktop')) { throw 'Unexpected resource path' }
        if (Test-Path -LiteralPath $resource) { Remove-Item -LiteralPath $resource }
        Pop-Location
    }
    foreach ($program in @('ffmpeg','ffprobe')) { Copy-Item -LiteralPath (Get-Command $program -ErrorAction Stop).Source -Destination (Join-Path $destination "runtime/bin/$program.exe") }
    Copy-Item -LiteralPath 'LICENSE' -Destination $destination
    Copy-Item -LiteralPath 'docs/DESKTOP.md' -Destination (Join-Path $destination 'DESKTOP-NOTES.md')
    Copy-Item -LiteralPath 'docs/COMPONENT-AND-CHRONOLOGY.md' -Destination (Join-Path $destination 'ENGINE-NOTES.md')
    $savedErrorPreference = $ErrorActionPreference
    try {
        # Windows PowerShell 5.1 can promote native stderr into a terminating
        # NativeCommandError even when FFmpeg is only printing its license/version.
        $ErrorActionPreference = 'Continue'
        & (Join-Path $destination 'runtime/bin/ffmpeg.exe') -L 2>&1 | Out-File -LiteralPath (Join-Path $destination 'FFmpeg-license.txt') -Encoding utf8
        if ($LASTEXITCODE -ne 0) { throw 'FFmpeg license capture failed' }
        & (Join-Path $destination 'runtime/bin/ffmpeg.exe') -version 2>&1 | Out-File -LiteralPath (Join-Path $destination 'FFmpeg-version.txt') -Encoding utf8
        if ($LASTEXITCODE -ne 0) { throw 'FFmpeg version capture failed' }
    } finally { $ErrorActionPreference = $savedErrorPreference }
    & (Join-Path $destination 'opensync-cli.exe') --help | Set-Content -LiteralPath (Join-Path $destination 'CLI-HELP.txt')
    if ($LASTEXITCODE -ne 0) { throw 'CLI smoke test failed' }
    git rev-parse HEAD | Set-Content -LiteralPath (Join-Path $destination 'source-checkpoint.txt')
    git status --porcelain | Set-Content -LiteralPath (Join-Path $destination 'source-worktree-status.txt')
    Get-ChildItem -LiteralPath $destination -Recurse -File | Get-FileHash -Algorithm SHA256 | Select-Object Path,Hash | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $destination 'package-hashes.json')
    Write-Output "Built OpenSync desktop and CLI: $destination"
} finally { $env:CGO_ENABLED=$priorCGO;Pop-Location }
