param([Parameter(Mandatory=$true)][string]$Output)
$ErrorActionPreference = "Stop"
$repository = Split-Path -Parent $PSScriptRoot
Push-Location -LiteralPath $repository
try {
    $destination = [System.IO.Path]::GetFullPath($Output)
    if ((Test-Path -LiteralPath $destination) -and (Get-ChildItem -LiteralPath $destination | Select-Object -First 1)) {
        throw "Choose an empty package output directory"
    }
    $binaryDirectory = Join-Path $destination "runtime/bin"
    New-Item -ItemType Directory -Force -Path $binaryDirectory | Out-Null
    foreach ($program in @("ffmpeg", "ffprobe")) {
        Copy-Item -LiteralPath (Get-Command $program -ErrorAction Stop).Source -Destination (Join-Path $binaryDirectory "$program.exe")
    }
    $priorCGO = $env:CGO_ENABLED
    try {
        $env:CGO_ENABLED = "0"
        go build -trimpath -o (Join-Path $destination "opensync.exe") ./cmd/opensync
        if ($LASTEXITCODE -ne 0) { throw "CLI build failed" }
    } finally { $env:CGO_ENABLED = $priorCGO }
    Copy-Item -LiteralPath "LICENSE" -Destination (Join-Path $destination "LICENSE")
    if (Test-Path -LiteralPath "docs/NATIVE-GO-MIGRATION.md") { Copy-Item -LiteralPath "docs/NATIVE-GO-MIGRATION.md" -Destination (Join-Path $destination "ENGINE-NOTES.md") }
    & (Join-Path $binaryDirectory "ffmpeg.exe") -L *> (Join-Path $destination "FFmpeg-license.txt")
    & (Join-Path $binaryDirectory "ffmpeg.exe") -version *> (Join-Path $destination "FFmpeg-version.txt")
    & (Join-Path $destination "opensync.exe") --help | Set-Content -LiteralPath (Join-Path $destination "CLI-HELP.txt")
    if ($LASTEXITCODE -ne 0) { throw "Native executable verification failed" }
    git rev-parse HEAD | Set-Content -LiteralPath (Join-Path $destination "source-checkpoint.txt")
    git status --porcelain | Set-Content -LiteralPath (Join-Path $destination "source-worktree-status.txt")
    Get-ChildItem -LiteralPath $destination -Recurse -File | Get-FileHash -Algorithm SHA256 | Select-Object Path,Hash | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $destination "package-hashes.json")
    Write-Output "Built native Go Windows package: $destination"
} finally { Pop-Location }
