param(
    [Parameter(Mandatory=$true)][string]$Output,
    [string]$BuildPython = "python"
)
$ErrorActionPreference = "Stop"
$repository = Split-Path -Parent $PSScriptRoot
Push-Location -LiteralPath $repository
try {
    $destination = [System.IO.Path]::GetFullPath($Output)
    if ((Test-Path -LiteralPath $destination) -and (Get-ChildItem -LiteralPath $destination | Select-Object -First 1)) {
        throw "Choose an empty package output directory"
    }
    $information = & $BuildPython -c 'import sys,pathlib,numpy,scipy,json; print(json.dumps({"base":sys.base_prefix,"version":str(sys.version_info.major)+str(sys.version_info.minor),"numpy":str(pathlib.Path(numpy.__file__).parent),"scipy":str(pathlib.Path(scipy.__file__).parent)}))' | ConvertFrom-Json
    if ($LASTEXITCODE -ne 0) { throw "Build Python must have NumPy and SciPy installed" }
    $runtimeDirectory = Join-Path $destination "runtime/python"
    $libraryDirectory = Join-Path $runtimeDirectory "Lib"
    $packagesDirectory = Join-Path $libraryDirectory "site-packages"
    $binaryDirectory = Join-Path $destination "runtime/bin"
    New-Item -ItemType Directory -Force -Path $packagesDirectory,$binaryDirectory | Out-Null
    Get-ChildItem -LiteralPath $information.base -File | Where-Object { $_.Name -eq "python.exe" -or $_.Extension -eq ".dll" -or $_.Name -eq "LICENSE.txt" } | ForEach-Object {
        Copy-Item -LiteralPath $_.FullName -Destination $runtimeDirectory
    }
    Copy-Item -LiteralPath (Join-Path $information.base "DLLs") -Destination (Join-Path $runtimeDirectory "DLLs") -Recurse
    Get-ChildItem -LiteralPath (Join-Path $information.base "Lib") | Where-Object { $_.Name -notin @("site-packages","__pycache__","test","ensurepip","idlelib","tkinter") } | ForEach-Object {
        Copy-Item -LiteralPath $_.FullName -Destination (Join-Path $libraryDirectory $_.Name) -Recurse
    }
    foreach ($package in @("numpy","scipy")) {
        $sourceDirectory = $information.$package
        Copy-Item -LiteralPath $sourceDirectory -Destination (Join-Path $packagesDirectory $package) -Recurse
        $siteDirectory = Split-Path -Parent $sourceDirectory
        Get-ChildItem -LiteralPath $siteDirectory | Where-Object { $_.Name -eq "$package.libs" -or $_.Name -like "$package-*.dist-info" } | ForEach-Object {
            Copy-Item -LiteralPath $_.FullName -Destination (Join-Path $packagesDirectory $_.Name) -Recurse
        }
    }
    @(".","DLLs","Lib","Lib/site-packages") | Set-Content -Encoding ascii -LiteralPath (Join-Path $runtimeDirectory ("python"+$information.version+"._pth"))
    foreach ($program in @("ffmpeg", "ffprobe")) {
        Copy-Item -LiteralPath (Get-Command $program -ErrorAction Stop).Source -Destination (Join-Path $binaryDirectory "$program.exe")
    }
    go build -trimpath -o (Join-Path $destination "opensync.exe") ./cmd/opensync
    if ($LASTEXITCODE -ne 0) { throw "CLI build failed" }
    if (Test-Path -LiteralPath "LICENSE") { Copy-Item -LiteralPath "LICENSE" -Destination (Join-Path $destination "LICENSE") }
    & (Join-Path $binaryDirectory "ffmpeg.exe") -L *> (Join-Path $destination "FFmpeg-license.txt")
    & (Join-Path $runtimeDirectory "python.exe") -c 'import sys,numpy,scipy,json; print(json.dumps({"python":sys.version,"numpy":numpy.__version__,"scipy":scipy.__version__,"isolated":sys.flags.isolated,"paths":sys.path},indent=2))' | Set-Content -LiteralPath (Join-Path $destination "runtime-versions.json")
    if ($LASTEXITCODE -ne 0) { throw "Isolated runtime verification failed" }
    git rev-parse HEAD | Set-Content -LiteralPath (Join-Path $destination "source-checkpoint.txt")
    Write-Output "Built self-contained Windows package: $destination"
} finally {
    Pop-Location
}
