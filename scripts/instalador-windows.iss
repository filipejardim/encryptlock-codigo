; Installer of the EncryptLock computer app: "Instalar EncryptLock.exe".
; Installs for the current user only (no administrator), adds Start menu
; and (optional) Desktop shortcuts, and a normal uninstaller in Windows
; Settings. Built by scripts\instalador-windows.ps1.

#ifndef SourceDir
  #error SourceDir must point to the Release folder
#endif
#ifndef OutDir
  #define OutDir "."
#endif
#ifndef AppVersion
  #define AppVersion "0.1.0"
#endif

[Setup]
AppId={{8C5E2A41-7B3D-4F19-A6E2-0D94C1B7E3F5}
AppName=EncryptLock
AppVersion={#AppVersion}
AppPublisher=EncryptLock
DefaultDirName={localappdata}\Programs\EncryptLock
DefaultGroupName=EncryptLock
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
OutputDir={#OutDir}
OutputBaseFilename=Instalar EncryptLock
SetupIconFile=..\mobile\windows\runner\resources\app_icon.ico
UninstallDisplayIcon={app}\EncryptLock.exe
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
CloseApplications=yes

[Languages]
Name: "pt"; MessagesFile: "compiler:Languages\Portuguese.isl"
Name: "en"; MessagesFile: "compiler:Default.isl"
Name: "ptbr"; MessagesFile: "compiler:Languages\BrazilianPortuguese.isl"
Name: "es"; MessagesFile: "compiler:Languages\Spanish.isl"
Name: "fr"; MessagesFile: "compiler:Languages\French.isl"
Name: "de"; MessagesFile: "compiler:Languages\German.isl"
Name: "it"; MessagesFile: "compiler:Languages\Italian.isl"
Name: "nl"; MessagesFile: "compiler:Languages\Dutch.isl"
Name: "pl"; MessagesFile: "compiler:Languages\Polish.isl"
Name: "tr"; MessagesFile: "compiler:Languages\Turkish.isl"
Name: "ru"; MessagesFile: "compiler:Languages\Russian.isl"
Name: "uk"; MessagesFile: "compiler:Languages\Ukrainian.isl"
Name: "ar"; MessagesFile: "compiler:Languages\Arabic.isl"
Name: "ja"; MessagesFile: "compiler:Languages\Japanese.isl"
Name: "ko"; MessagesFile: "compiler:Languages\Korean.isl"
Name: "th"; MessagesFile: "compiler:Languages\Thai.isl"

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"

[Files]
Source: "{#SourceDir}\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
Name: "{group}\EncryptLock"; Filename: "{app}\EncryptLock.exe"
Name: "{group}\{cm:UninstallProgram,EncryptLock}"; Filename: "{uninstallexe}"
Name: "{userdesktop}\EncryptLock"; Filename: "{app}\EncryptLock.exe"; Tasks: desktopicon

[Run]
Filename: "{app}\EncryptLock.exe"; Description: "{cm:LaunchProgram,EncryptLock}"; Flags: nowait postinstall skipifsilent
; "Update" inside the app runs this installer without questions: the new
; app opens by itself at the end (its LockKeeper, if it was on, comes back).
Filename: "{app}\EncryptLock.exe"; Flags: nowait; Check: WizardSilent

[UninstallRun]
; Stop this PC's LockKeeper ("Make this PC a LockKeeper"); its guarantee
; stays on the network and the owner keeps it.
Filename: "taskkill.exe"; Parameters: "/IM lockkeeper.exe /F"; Flags: runhidden; RunOnceId: "StopLockKeeper"
; Its direct connection through Cloudflare (only the app's own copy, never
; another cloudflared on this computer).
Filename: "powershell.exe"; Parameters: "-NoProfile -Command ""Get-Process cloudflared -ErrorAction SilentlyContinue | Where-Object {{ $_.Path -like '{app}\*' }} | Stop-Process -Force"""; Flags: runhidden; RunOnceId: "StopDoor"
; Turn off "Start with Windows" (the wallet itself stays in the user's
; data folder; it only opens with the password or the 24 words).
Filename: "reg.exe"; Parameters: "delete HKCU\Software\Microsoft\Windows\CurrentVersion\Run /v EncryptLock /f"; Flags: runhidden; RunOnceId: "RemoveStartup"

[Code]
// Before copying: stop this app's own LockKeeper and its direct connection
// (only the programs inside the app's folder, never another copy on this
// computer). 25 Oct 2026: a friend installed by hand with the LockKeeper on,
// Windows kept the old lockkeeper.exe ("files in use") and it stayed stuck.
// The app starts it again when it opens at the end.
function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  Code: Integer;
begin
  // 64-bit PowerShell: a 32-bit one cannot see the path of 64-bit programs.
  Exec(ExpandConstant('{sys}\WindowsPowerShell\v1.0\powershell.exe'), '-NoProfile -Command "Get-Process lockkeeper, cloudflared -ErrorAction SilentlyContinue | Where-Object { $_.Path -like ''' + ExpandConstant('{app}') + '\*'' } | Stop-Process -Force"', '', SW_HIDE, ewWaitUntilTerminated, Code);
  Sleep(1500);
  Result := '';
end;
