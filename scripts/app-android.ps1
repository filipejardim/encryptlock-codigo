# Builds the EncryptLock Android app (test version: opens straight inside,
# with a test wallet, connected to the LockKeeper on this PC) and puts the APK on the
# Desktop as EncryptLock-teste.apk.
#
# 1. Builds the Go wallet core (same code as the network) as an Android library.
# 2. Builds the Flutter app for phones (arm64).
#
# Needs Go, Flutter (C:\src\flutter) and the Android SDK with NDK 28.
# Usage:  powershell -ExecutionPolicy Bypass -File scripts\app-android.ps1
#   USDC and POL from the mini-Polygon of this PC (scripts\polygon-local.ps1 first), like the PC app:  ... -PolygonLocal
#   as a new user sees it (welcome screen, create wallet, password, 24 words),
#   still on the test network of this PC:  ... -Normal
#   final version fixed to the real network (codes from mobile/tool/genesis_pin.dart):
#   ... -ChainId encryptlock-1 -GenesisHash <64 letters>
param([switch]$Normal, [switch]$PolygonLocal, [string]$ChainId, [string]$GenesisHash)

$ErrorActionPreference = "Continue"
$root = Split-Path -Parent $PSScriptRoot
$env:Path = "C:\src\flutter\bin;" + [Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [Environment]::GetEnvironmentVariable("Path", "User")

Write-Host "1/2 A compilar o nucleo Go da carteira para Android (demora alguns minutos)..." -ForegroundColor Green
& "C:\Program Files\Git\bin\bash.exe" (Join-Path $root "scripts/mobilecore.sh")
if ($LASTEXITCODE -ne 0) { throw "Falhou o nucleo Go." }

Write-Host "2/2 A compilar a app Android..." -ForegroundColor Green
Push-Location (Join-Path $root "mobile")
# Test build: opens straight into a test wallet (no password, no 24 words)
# and talks to the LockKeeper on this PC, through the Wi-Fi address.
$ip = (Get-NetIPAddress -AddressFamily IPv4 | Where-Object { $_.IPAddress -notmatch "^(127|169)." -and $_.PrefixOrigin -ne "WellKnown" } | Select-Object -First 1).IPAddress
Write-Host "   LockKeeper deste PC: http://$($ip):1317" -ForegroundColor Green
# The final app (fixed to the real network) finds the network through the
# website's door, at home or away; test builds use this PC on the Wi-Fi.
$defines = @()
if (-not $ChainId) { $defines += "--dart-define=LOCKKEEPER=http://$($ip):1317" }
if (-not $Normal) { $defines += "--dart-define=DEMO=true" }
if ($PolygonLocal) { $defines += "--dart-define=POLYGON=local" }
if ($ChainId) { $defines += "--dart-define=CHAIN_ID=$ChainId" }
if ($GenesisHash) { $defines += "--dart-define=GENESIS_HASH=$GenesisHash" }
# Code scrambled (names removed): harder to read and copy. The map to read
# crash reports stays here, in build/symbols (never published).
flutter build apk --release --split-per-abi --obfuscate --split-debug-info=build/symbols/android @defines
$ok = $LASTEXITCODE -eq 0
Pop-Location
if (-not $ok) { throw "Falhou a app." }

$name = if ($Normal) { "EncryptLock-telemovel.apk" } else { "EncryptLock-teste.apk" }
$apk = Join-Path $env:USERPROFILE "Desktop\$name"
Copy-Item (Join-Path $root "mobile\build\app\outputs\flutter-apk\app-arm64-v8a-release.apk") $apk -Force
Write-Host "Pronto: $apk" -ForegroundColor Green
