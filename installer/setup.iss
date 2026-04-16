; ============================================================
;  LAGGADO — Inno Setup Script
;  Autor: kaueramone.dev
;  Versão: 0.2.0 — Abril 2026
;
;  Pré-requisitos para compilar este instalador:
;  1. Inno Setup 6.x instalado (https://jrsoftware.org/isinfo.php)
;  2. LAGGADO.exe compilado em ..\dist\LAGGADO.exe
;  3. Arquivo terms_pt.rtf nesta pasta
;
;  Para compilar: abra este arquivo no Inno Setup e clique em Build > Compile
; ============================================================

#define AppName      "LAGGADO"
#define AppVersion   "0.2.0"
#define AppPublisher "kaueramone.dev"
#define AppURL       "https://github.com/kaueramone/laggado"
#define AppExeName   "LAGGADO.exe"
#define AppContact   "kaueramone@live.com"
#define AppCopyright "Copyright (C) 2026 KAUERAMONE.DEV"

[Setup]
; Identificador único da aplicação (não altere para manter atualizações corretas)
AppId={{7A3F1E8C-2B4D-4A6E-9C1F-8D5B3E7A0C2F}}
AppName={#AppName}
AppVersion={#AppVersion}
AppVerName={#AppName} v{#AppVersion}
AppPublisher={#AppPublisher}
AppPublisherURL={#AppURL}
AppSupportURL={#AppURL}/issues
AppUpdatesURL={#AppURL}/releases
AppContact={#AppContact}
AppCopyright={#AppCopyright}

; Diretório de instalação padrão
DefaultDirName={autopf}\{#AppName}
DefaultGroupName={#AppName}
DisableProgramGroupPage=yes

; Arquivo de licença
LicenseFile=terms_pt.rtf

; Ícone do instalador
SetupIconFile=..\assets\laggado.ico

; Requisito mínimo: Windows 10 (versão 1903 ou superior)
MinVersion=10.0.18362

; O instalador precisa de direitos de administrador (necessário para WireGuard e rotas)
PrivilegesRequired=admin
PrivilegesRequiredOverridesAllowed=commandline

; Diretório de saída do instalador
OutputDir=..\dist
OutputBaseFilename=LAGGADO-Setup-v{#AppVersion}

; Compressão
Compression=lzma2/ultra64
SolidCompression=yes
LZMAUseSeparateProcess=yes

; Aparência do wizard
WizardStyle=modern
WizardSizePercent=120

; Não criar RunProgram no registro (app não precisa de COM etc.)
ChangesAssociations=no

; Metadados do instalador
VersionInfoVersion={#AppVersion}.0
VersionInfoCompany={#AppPublisher}
VersionInfoDescription={#AppName} Setup
VersionInfoCopyright={#AppCopyright}

[Languages]
Name: "brazilianportuguese"; MessagesFile: "compiler:Languages\BrazilianPortuguese.isl"
Name: "english"; MessagesFile: "compiler:Default.isl"

[CustomMessages]
brazilianportuguese.WireGuardNotFound=O WireGuard não foi encontrado no seu sistema.%n%nO LAGGADO precisa do WireGuard para criar o túnel criptografado.%n%nClique em OK para baixar o WireGuard agora, ou Cancelar para instalar depois manualmente.
brazilianportuguese.WireGuardTitle=WireGuard Necessário
brazilianportuguese.InstallingMsg=Instalando {#AppName}...
brazilianportuguese.AdminRequired=O LAGGADO requer permissões de Administrador para gerenciar rotas de rede.%nExecute o instalador como Administrador.
english.WireGuardNotFound=WireGuard was not found on your system.%n%nLAGGADO needs WireGuard to create the encrypted tunnel.%n%nClick OK to download WireGuard now, or Cancel to install it later manually.
english.WireGuardTitle=WireGuard Required
english.InstallingMsg=Installing {#AppName}...
english.AdminRequired=LAGGADO requires Administrator permissions to manage network routes.%nPlease run the installer as Administrator.

[Tasks]
Name: "desktopicon";   Description: "Criar ícone na Área de Trabalho";     GroupDescription: "Atalhos:"
Name: "startmenuicon"; Description: "Criar atalho no Menu Iniciar";        GroupDescription: "Atalhos:"
Name: "autostart";     Description: "Iniciar LAGGADO automaticamente com o Windows"; GroupDescription: "Opções:"; Flags: unchecked

[Files]
; Executável principal (compilado em ..\dist\LAGGADO.exe)
Source: "..\dist\LAGGADO.exe";       DestDir: "{app}";          Flags: ignoreversion
Source: "..\dist\wg.exe";            DestDir: "{app}";          Flags: ignoreversion skipifsourcedoesntexist
Source: "..\dist\wireguard.exe";     DestDir: "{app}";          Flags: ignoreversion skipifsourcedoesntexist
Source: "..\README.md";              DestDir: "{app}";          Flags: ignoreversion skipifsourcedoesntexist
Source: "terms_pt.rtf";              DestDir: "{app}\docs";     Flags: ignoreversion

[Icons]
; Atalho no Menu Iniciar
Name: "{group}\{#AppName}";              Filename: "{app}\{#AppExeName}"; Tasks: startmenuicon
Name: "{group}\Desinstalar {#AppName}";  Filename: "{uninstallexe}";      Tasks: startmenuicon

; Atalho na Área de Trabalho
Name: "{autodesktop}\{#AppName}";        Filename: "{app}\{#AppExeName}"; Tasks: desktopicon

[Registry]
; Autostart com o Windows (opcional, desativado por padrão)
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "{#AppName}"; ValueData: """{app}\{#AppExeName}"""; Tasks: autostart; Flags: uninsdeletevalue

; Armazena a versão instalada para referência futura
Root: HKCU; Subkey: "Software\LAGGADO"; ValueType: string; ValueName: "Version"; ValueData: "{#AppVersion}"; Flags: uninsdeletekey
Root: HKCU; Subkey: "Software\LAGGADO"; ValueType: string; ValueName: "InstallPath"; ValueData: "{app}"; Flags: uninsdeletekey

[Run]
; Lançar LAGGADO após a instalação (opção desmarcada por padrão)
Filename: "{app}\{#AppExeName}"; Description: "Iniciar {#AppName} agora"; Flags: nowait postinstall skipifsilent

[UninstallRun]
; Garantir que o LAGGADO fecha antes de desinstalar
Filename: "taskkill.exe"; Parameters: "/F /IM {#AppExeName}"; Flags: runhidden; RunOnceId: "KillApp"

[Code]
// ============================================================
//  Seção de código Pascal — verificações e lógica customizada
// ============================================================

// Verifica se o WireGuard está instalado no sistema
function IsWireGuardInstalled(): Boolean;
var
  WgPath1, WgPath2: String;
begin
  WgPath1 := ExpandConstant('{pf}\WireGuard\wireguard.exe');
  WgPath2 := ExpandConstant('{pf32}\WireGuard\wireguard.exe');
  Result := FileExists(WgPath1) or FileExists(WgPath2);
end;

// Verifica se o Windows 10/11 está sendo usado
function IsWindows10OrLater(): Boolean;
var
  Version: TWindowsVersion;
begin
  GetWindowsVersionEx(Version);
  Result := (Version.Major >= 10);
end;

// Executado antes de mostrar o wizard
function InitializeSetup(): Boolean;
begin
  Result := True;

  // Verifica versão do Windows
  if not IsWindows10OrLater() then
  begin
    MsgBox(
      'O LAGGADO requer Windows 10 ou superior.' + #13#10 +
      'Por favor, atualize o seu sistema operacional.',
      mbCriticalError, MB_OK
    );
    Result := False;
    Exit;
  end;
end;

// Executado após a instalação concluída
procedure CurStepChanged(CurStep: TSetupStep);
var
  ErrorCode: Integer;
begin
  if CurStep = ssPostInstall then
  begin
    // Verifica se o WireGuard está instalado
    if not IsWireGuardInstalled() then
    begin
      if MsgBox(
        CustomMessage('WireGuardNotFound'),
        mbConfirmation, MB_OKCANCEL
      ) = IDOK then
      begin
        // Abre o site oficial do WireGuard para download
        ShellExec('open', 'https://www.wireguard.com/install/', '', '', SW_SHOW, ewNoWait, ErrorCode);
      end;
    end;
  end;
end;

