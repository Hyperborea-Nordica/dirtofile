# Cross-check tar listings: Go vs Rust vs golden (Windows / PowerShell).
$ErrorActionPreference = "Stop"

$Root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
$Fixture = Join-Path $Root "testdata\fixtures\standard"
$Golden = Join-Path $Root "testdata\golden\standard-recursive.txt"
$OutGo = [System.IO.Path]::GetTempFileName()
$OutRs = [System.IO.Path]::GetTempFileName()

try {
    $BinDir = Join-Path $Root "bin"
    New-Item -ItemType Directory -Force -Path $BinDir | Out-Null

    Write-Host "==> Building Go"
    Push-Location (Join-Path $Root "go")
    go build -o (Join-Path $BinDir "dirtofile-go.exe") .
    Pop-Location

    Write-Host "==> Building Rust"
    Push-Location (Join-Path $Root "rust")
    cargo build -q
    Pop-Location
    $RsBin = Join-Path $Root "rust\target\debug\dirtofile.exe"

    $GoBin = Join-Path $Root "bin\dirtofile-go.exe"
    & $GoBin -d $Fixture -o $OutGo --format tar -r -q --force
    & $RsBin -d $Fixture -o $OutRs --format tar -r -q --force

    function Get-TarNames($path) {
        tar -tf $path | Sort-Object
    }

    $want = Get-Content $Golden | Sort-Object
    $go = Get-TarNames $OutGo
    $rs = Get-TarNames $OutRs

    if (($go -join "`n") -ne ($want -join "`n")) {
        throw "Go listing differs from golden`nGo: $($go -join ', ')`nWant: $($want -join ', ')"
    }
    if (($rs -join "`n") -ne ($want -join "`n")) {
        throw "Rust listing differs from golden"
    }
    if (($go -join "`n") -ne ($rs -join "`n")) {
        throw "Go and Rust listings differ"
    }

    Write-Host "Golden cross-check passed."
}
finally {
    Remove-Item -Force $OutGo, $OutRs -ErrorAction SilentlyContinue
}
