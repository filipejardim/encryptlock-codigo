# Builds the EncryptLock computer app (Windows) and puts it on the Desktop:
# folder "EncryptLock-PC" + shortcut "EncryptLock (PC)".
#
# Same app as the phone (same screens, theme, 21 languages, rules), with the
# Go wallet core next to it (encryptlock-core.exe) and the LockKeeper
# (lockkeeper.exe, for "Make this PC a LockKeeper"). Test version: opens
# straight inside a test wallet and talks to the LockKeeper on this PC.
#
# Needs Flutter (C:\src\flutter), Go and Visual Studio Build Tools (C++).
# Usage:  powershell -ExecutionPolicy Bypass -File scripts\app-windows.ps1
#   with the Polygon test chain on this PC ("Buy" works):  ... -PolygonLocal
#   as a new user sees it (welcome screen, create or recover a wallet):  ... -Normal
#   final version fixed to the real network (codes from mobile/tool/genesis_pin.dart):
#   ... -ChainId encryptlock-1 -GenesisHash <64 letters>
param([switch]$Normal, [switch]$PolygonLocal, [string]$ChainId, [string]$GenesisHash)

$ErrorActionPreference = "Continue"
$root = Split-Path -Parent $PSScriptRoot
$env:Path = "C:\src\flutter\bin;" + [Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [Environment]::GetEnvironmentVariable("Path", "User")

Write-Host "1/3 A compilar a app de Windows..." -ForegroundColor Green
Push-Location (Join-Path $root "mobile")
$defines = @()
if (-not $Normal) { $defines += "--dart-define=DEMO=true" }
if ($PolygonLocal) { $defines += "--dart-define=POLYGON=local" }
if ($ChainId) { $defines += "--dart-define=CHAIN_ID=$ChainId" }
if ($GenesisHash) { $defines += "--dart-define=GENESIS_HASH=$GenesisHash" }
# Code scrambled (names removed): harder to read and copy. The map to read
# crash reports stays here, in build/symbols (never published).
flutter build windows --release --obfuscate --split-debug-info=build/symbols/windows @defines
$ok = $LASTEXITCODE -eq 0
Pop-Location
if (-not $ok) { throw "Falhou a app." }
$release = Join-Path $root "mobile\build\windows\x64\runner\Release"

Write-Host "2/3 A compilar o nucleo Go da carteira..." -ForegroundColor Green
Push-Location $root
go build -ldflags="-s -w" -o (Join-Path $release "encryptlock-core.exe") ./cmd/encryptlock-core
if ($LASTEXITCODE -eq 0) {
    # The LockKeeper, for "Make this PC a LockKeeper".
    go build -ldflags="-s -w" -o (Join-Path $release "lockkeeper.exe") ./cmd/lockkeeper
}
$ok = $LASTEXITCODE -eq 0
Pop-Location
if (-not $ok) { throw "Falhou o nucleo." }

# Cloudflare's program, for a LockKeeper that joins from another home (its
# direct connection to the network goes through the network's door). Fixed
# version, checked against Cloudflare's published SHA-256.
$cfVersion = "2026.10.0"
$cfSha = "86aee4017b26625cee8484c113558f48effa4cd47f7aa05fcf425604e5d2b23c"
$cfCache = Join-Path $env:LOCALAPPDATA "EncryptLock\cloudflared-$cfVersion.exe"
if (-not (Test-Path $cfCache) -or (Get-FileHash $cfCache -Algorithm SHA256).Hash -ne $cfSha) {
    New-Item -ItemType Directory -Force (Split-Path $cfCache) | Out-Null
    Invoke-WebRequest -UseBasicParsing "https://github.com/cloudflare/cloudflared/releases/download/$cfVersion/cloudflared-windows-amd64.exe" -OutFile $cfCache
}
if ((Get-FileHash $cfCache -Algorithm SHA256).Hash -ne $cfSha) { Remove-Item $cfCache; throw "O programa do Cloudflare nao confere (SHA-256)." }
Copy-Item $cfCache (Join-Path $release "cloudflared.exe")

Write-Host "3/3 A copiar para o Ambiente de Trabalho..." -ForegroundColor Green
Get-Process EncryptLock, encryptlock_wallet, encryptlock-core -ErrorAction SilentlyContinue | Stop-Process -Force
$dest = Join-Path ([Environment]::GetFolderPath("Desktop")) "EncryptLock-PC"
# This PC's LockKeeper from the previous copy (not the test network's).
Get-Process lockkeeper -ErrorAction SilentlyContinue | Where-Object { $_.Path -like "$dest*" } | Stop-Process -Force
# Windows may keep the files locked for a few seconds after the programs
# stop: try again, and never copy into a folder that is still there (it
# would end up inside it, as EncryptLock-PC\Release).
for ($i = 0; $i -lt 10 -and (Test-Path $dest); $i++) {
    Start-Sleep -Seconds 2
    Remove-Item -Recurse -Force $dest -ErrorAction SilentlyContinue
}
if (Test-Path $dest) { throw "Nao foi possivel apagar a copia antiga ($dest)." }
Copy-Item $release $dest -Recurse
Rename-Item (Join-Path $dest "encryptlock_wallet.exe") "EncryptLock.exe"
$shell = New-Object -ComObject WScript.Shell
$lnk = $shell.CreateShortcut((Join-Path ([Environment]::GetFolderPath("Desktop")) "EncryptLock (PC).lnk"))
$lnk.TargetPath = Join-Path $dest "EncryptLock.exe"
$lnk.WorkingDirectory = $dest
$lnk.Save()
Write-Host "Pronto: atalho 'EncryptLock (PC)' no Ambiente de Trabalho." -ForegroundColor Green
