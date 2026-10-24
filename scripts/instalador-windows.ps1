# Builds the installer "Instalar EncryptLock.exe" on the Desktop.
# It installs the computer app like any Windows program: Start menu,
# optional Desktop shortcut, uninstall in Settings > Apps. No administrator.
#
# Needs Inno Setup 6 (free) and everything app-windows.ps1 needs.
# Usage:  powershell -ExecutionPolicy Bypass -File scripts\instalador-windows.ps1

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot

# 1. Build the app (same as the test version on the Desktop).
& powershell -ExecutionPolicy Bypass -File (Join-Path $PSScriptRoot "app-windows.ps1")
if ($LASTEXITCODE -ne 0) { throw "Falhou a app." }
$app = Join-Path ([Environment]::GetFolderPath("Desktop")) "EncryptLock-PC"

# 2. Package it.
$iscc = @(
  "$env:LOCALAPPDATA\Programs\Inno Setup 6\ISCC.exe",
  "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe",
  "$env:ProgramFiles\Inno Setup 6\ISCC.exe"
) | Where-Object { Test-Path $_ } | Select-Object -First 1
if (-not $iscc) { throw "Falta o Inno Setup 6: winget install JRSoftware.InnoSetup" }
Write-Host "A criar o instalador..." -ForegroundColor Green
$desktop = [Environment]::GetFolderPath("Desktop")
& $iscc /Q "/DSourceDir=$app" "/DOutDir=$desktop" (Join-Path $PSScriptRoot "instalador-windows.iss")
if ($LASTEXITCODE -ne 0) { throw "Falhou o instalador." }
Write-Host "Pronto: 'Instalar EncryptLock.exe' no Ambiente de Trabalho." -ForegroundColor Green
