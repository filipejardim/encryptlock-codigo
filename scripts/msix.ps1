# Builds the EncryptLock computer app as a Microsoft Store package (MSIX):
# Desktop\EncryptLock.msix, with the same files as the PC app
# (scripts\app-windows.ps1 first: it builds them in mobile\build\...\Release).
#
# Inside the package:
#   - EncryptLock.exe (the app), encryptlock-core.exe, lockkeeper.exe,
#     cloudflared.exe (checked by SHA-256 in app-windows.ps1);
#   - "Switch on by itself when Windows starts" = the package's startup task
#     "LockKeeperStart" (off until the owner switches it on in the app): it
#     runs lockkeeper.exe with no arguments, which then does pc-autostart
#     (cmd/lockkeeper StartupArgs). A Startup-folder shortcut cannot start a
#     program inside a package.
#   - "Start with Windows" (the app itself, for the alerts) = startup task
#     "AppStart" (off until switched on), instead of the registry Run line.
#
# Test (this PC): signed with a test certificate made here (Desktop\
# EncryptLock-teste.cer). To install it the founder trusts that certificate
# once (as administrator) and double-clicks the .msix - see PROGRESSO.md.
# Store: ... -Loja -IdentityName <from Partner Center> -Publisher "CN=<from
# Partner Center>" -PublisherDisplayName <name>: not signed here (the Store
# signs it).
#
# Usage:  powershell -ExecutionPolicy Bypass -File scripts\msix.ps1 [-Versao 1.0.0.0]
param(
    [string]$Versao = "1.0.0.0",
    [string]$IdentityName = "EncryptLock.Teste",
    [string]$Publisher = "CN=EncryptLock Teste",
    [string]$PublisherDisplayName = "EncryptLock",
    [switch]$Loja
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$release = Join-Path $root "mobile\build\windows\x64\runner\Release"
$layout = Join-Path $root "mobile\build\msix\layout"
$desktop = [Environment]::GetFolderPath("Desktop")
$out = Join-Path $desktop "EncryptLock.msix"
$kit = Get-ChildItem "${env:ProgramFiles(x86)}\Windows Kits\10\bin\10.*\x64\makeappx.exe" | Sort-Object FullName | Select-Object -Last 1
if (-not $kit) { throw "Falta o Windows SDK (makeappx.exe)." }
$makeappx = $kit.FullName
$signtool = Join-Path $kit.DirectoryName "signtool.exe"
foreach ($f in "encryptlock_wallet.exe", "encryptlock-core.exe", "lockkeeper.exe", "cloudflared.exe") {
    if (-not (Test-Path (Join-Path $release $f))) { throw "Falta $f em $release : corre primeiro scripts\app-windows.ps1" }
}
if ($Versao -notmatch '^\d+\.\d+\.\d+\.0$') { throw "Versao tem de ser como 1.0.0.0 (o ultimo numero e sempre 0 na Store)." }

Write-Host "1/4 A preparar os ficheiros do pacote..." -ForegroundColor Green
if (Test-Path $layout) { Remove-Item -Recurse -Force $layout }
Copy-Item $release $layout -Recurse
Rename-Item (Join-Path $layout "encryptlock_wallet.exe") "EncryptLock.exe"
$assets = New-Item -ItemType Directory -Force (Join-Path $layout "Assets")
Add-Type -AssemblyName System.Drawing
$icon = [Drawing.Image]::FromFile((Join-Path $root "design\icone-encryptlock.png"))
foreach ($s in @(@("Square44x44Logo.png", 44), @("Square44x44Logo.targetsize-44_altform-unplated.png", 44), @("Square150x150Logo.png", 150), @("StoreLogo.png", 50))) {
    $bmp = New-Object Drawing.Bitmap $s[1], $s[1]
    $g = [Drawing.Graphics]::FromImage($bmp)
    $g.InterpolationMode = [Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
    $g.DrawImage($icon, 0, 0, $s[1], $s[1])
    $bmp.Save((Join-Path $assets $s[0]), [Drawing.Imaging.ImageFormat]::Png)
    $g.Dispose(); $bmp.Dispose()
}
$icon.Dispose()

# The app's 21 languages (Store listing).
$langs = "en-us", "pt-pt", "pt-br", "es-es", "fr-fr", "de-de", "it-it", "nl-nl", "pl-pl", "ru-ru", "uk-ua", "tr-tr", "ar-sa", "hi-in", "id-id", "ja-jp", "ko-kr", "th-th", "vi-vn", "zh-cn", "zh-tw"
$resources = ($langs | ForEach-Object { "    <Resource Language=`"$_`" />" }) -join "`n"
$manifest = @"
<?xml version="1.0" encoding="utf-8"?>
<Package xmlns="http://schemas.microsoft.com/appx/manifest/foundation/windows10"
         xmlns:uap="http://schemas.microsoft.com/appx/manifest/uap/windows10"
         xmlns:desktop="http://schemas.microsoft.com/appx/manifest/desktop/windows10"
         xmlns:desktop6="http://schemas.microsoft.com/appx/manifest/desktop/windows10/6"
         xmlns:rescap="http://schemas.microsoft.com/appx/manifest/foundation/windows10/restrictedcapabilities"
         IgnorableNamespaces="uap desktop desktop6 rescap">
  <Identity Name="$IdentityName" Publisher="$Publisher" Version="$Versao" ProcessorArchitecture="x64" />
  <Properties>
    <DisplayName>EncryptLock</DisplayName>
    <PublisherDisplayName>$PublisherDisplayName</PublisherDisplayName>
    <Logo>Assets\StoreLogo.png</Logo>
    <!-- One real LockKeeper folder (%LOCALAPPDATA%\EncryptLock), never a private
         copy: two copies of the block-signing key could sign twice. -->
    <desktop6:FileSystemWriteVirtualization>disabled</desktop6:FileSystemWriteVirtualization>
  </Properties>
  <Dependencies>
    <TargetDeviceFamily Name="Windows.Desktop" MinVersion="10.0.19041.0" MaxVersionTested="10.0.26100.0" />
  </Dependencies>
  <Resources>
$resources
  </Resources>
  <Applications>
    <Application Id="EncryptLock" Executable="EncryptLock.exe" EntryPoint="Windows.FullTrustApplication">
      <uap:VisualElements DisplayName="EncryptLock" Description="EncryptLock wallet and LockKeeper"
                          BackgroundColor="transparent" Square150x150Logo="Assets\Square150x150Logo.png" Square44x44Logo="Assets\Square44x44Logo.png" />
      <Extensions>
        <uap:Extension Category="windows.protocol">
          <uap:Protocol Name="encryptlock" />
        </uap:Extension>
        <desktop:Extension Category="windows.startupTask" Executable="lockkeeper.exe" EntryPoint="Windows.FullTrustApplication">
          <desktop:StartupTask TaskId="LockKeeperStart" Enabled="false" DisplayName="EncryptLock LockKeeper" />
        </desktop:Extension>
        <desktop:Extension Category="windows.startupTask" Executable="EncryptLock.exe" EntryPoint="Windows.FullTrustApplication">
          <desktop:StartupTask TaskId="AppStart" Enabled="false" DisplayName="EncryptLock" />
        </desktop:Extension>
      </Extensions>
    </Application>
  </Applications>
  <Capabilities>
    <Capability Name="internetClient" />
    <Capability Name="internetClientServer" />
    <Capability Name="privateNetworkClientServer" />
    <rescap:Capability Name="runFullTrust" />
    <rescap:Capability Name="unvirtualizedResources" />
  </Capabilities>
</Package>
"@
[IO.File]::WriteAllText((Join-Path $layout "AppxManifest.xml"), $manifest)

Write-Host "2/4 A fazer o pacote..." -ForegroundColor Green
if (Test-Path $out) { Remove-Item $out }
& $makeappx pack /d $layout /p $out /o | Out-Null
if ($LASTEXITCODE -ne 0) { & $makeappx pack /d $layout /p $out /o; throw "makeappx falhou." }

# The test package on this PC is registered from $layout (Developer Mode).
# Rebuilding wipes that folder, which leaves Windows with a stale
# registration (old Start icon, app does not open): register it again.
$registered = Get-AppxPackage EncryptLock.Teste
if ($registered -and $registered.InstallLocation -eq $layout) {
    Add-AppxPackage -Register (Join-Path $layout "AppxManifest.xml") -ForceApplicationShutdown -ForceUpdateFromAnyVersion
    Write-Host "   pacote de teste registado outra vez neste PC"
}

if ($Loja) {
    Write-Host "Pronto (Store, sem assinatura - a Microsoft assina): $out" -ForegroundColor Green
    return
}
Write-Host "3/4 A assinar com o certificado de teste..." -ForegroundColor Green
$cert = Get-ChildItem Cert:\CurrentUser\My -CodeSigningCert | Where-Object { $_.Subject -eq $Publisher -and $_.NotAfter -gt (Get-Date) } | Select-Object -First 1
if (-not $cert) {
    $cert = New-SelfSignedCertificate -Type Custom -Subject $Publisher -KeyUsage DigitalSignature -FriendlyName "EncryptLock teste" `
        -CertStoreLocation Cert:\CurrentUser\My -TextExtension @("2.5.29.37={text}1.3.6.1.5.5.7.3.3", "2.5.29.19={text}")
}
Export-Certificate -Cert $cert -FilePath (Join-Path $desktop "EncryptLock-teste.cer") | Out-Null
& $signtool sign /fd SHA256 /sha1 $cert.Thumbprint $out | Out-Null
if ($LASTEXITCODE -ne 0) { throw "signtool falhou." }

Write-Host "4/4 A verificar..." -ForegroundColor Green
$sig = Get-AuthenticodeSignature $out
Write-Host "   assinatura: $($sig.SignerCertificate.Subject) ($($sig.Status))"
Write-Host "Pronto: $out ($([math]::Round((Get-Item $out).Length / 1MB, 1)) MB) e EncryptLock-teste.cer no Ambiente de Trabalho." -ForegroundColor Green
