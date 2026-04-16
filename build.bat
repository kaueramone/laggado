@echo off
setlocal EnableDelayedExpansion

:: ============================================================
::  LAGGADO - Script de Build para Windows
::  Autor: Kaue Da Costa Pacheco
::  Uso: execute este .bat na pasta raiz do projeto
:: ============================================================

echo.
echo ================================================================
echo  LAGGADO Build Script v0.2.0
echo ================================================================
echo.

:: Verifica se o Go esta instalado
where go >nul 2>&1
if %ERRORLEVEL% NEQ 0 (
    echo [ERRO] Go nao encontrado no PATH.
    echo        Baixe em: https://go.dev/dl/
    pause
    exit /b 1
)

:: Verifica se o GCC esta instalado
where gcc >nul 2>&1
if %ERRORLEVEL% NEQ 0 (
    echo [ERRO] GCC nao encontrado no PATH.
    echo        Instale o MSYS2 e rode: pacman -S mingw-w64-x86_64-gcc
    echo        Adicione C:\msys64\mingw64\bin ao PATH e reabra o terminal.
    pause
    exit /b 1
)

for /f "tokens=*" %%i in ('go version') do set GOVERSION=%%i
echo [GO]   !GOVERSION!
for /f "tokens=*" %%i in ('gcc --version 2^>^&1') do (
    set GCCVER=%%i
    goto :gccfound
)
:gccfound
echo [GCC]  !GCCVER!

set OUTDIR=dist
set GUIBIN=LAGGADO.exe
set VERSION=0.2.0

if not exist "%OUTDIR%" mkdir "%OUTDIR%"

echo.
echo [1/4] Baixando dependencias...
go mod download
if %ERRORLEVEL% NEQ 0 (
    echo [ERRO] Falha ao baixar dependencias.
    pause
    exit /b 1
)
echo       OK

echo.
echo [2/4] Compilando recursos Windows (icone + versao)...
set WINDRES=C:\msys64\mingw64\bin\windres.exe
if exist "%WINDRES%" (
    "%WINDRES%" -o cmd\laggado-gui\resource.syso cmd\laggado-gui\resource.rc
    if %ERRORLEVEL% NEQ 0 (
        echo [AVISO] windres falhou - o .exe ficara sem icone customizado.
    ) else (
        echo       OK - resource.syso gerado
    )
) else (
    echo [AVISO] windres nao encontrado em %WINDRES% - pulando icone.
)

echo.
echo [3/4] Compilando LAGGADO.exe...
set CGO_ENABLED=1
set GOOS=windows
set GOARCH=amd64

go build -ldflags="-s -w -H windowsgui -X main.Version=%VERSION%" -o "%OUTDIR%\%GUIBIN%" .\cmd\laggado-gui

if %ERRORLEVEL% NEQ 0 (
    echo [ERRO] Falha ao compilar.
    pause
    exit /b 1
)
echo       OK

echo.
echo [4/4] Copiando WireGuard (se instalado)...
set WG_PATH=C:\Program Files\WireGuard
if exist "%WG_PATH%\wg.exe" (
    copy /Y "%WG_PATH%\wg.exe"        "%OUTDIR%\wg.exe"        >nul
    copy /Y "%WG_PATH%\wireguard.exe" "%OUTDIR%\wireguard.exe" >nul
    echo       WireGuard copiado de "%WG_PATH%"
) else (
    echo       WireGuard nao encontrado - ok, instalador vai verificar
)

echo.
echo ================================================================
echo  Deseja compilar o instalador Inno Setup agora? (S/N)
echo ================================================================
set /p COMPILE_INSTALLER="> "
if /i "!COMPILE_INSTALLER!"=="S" (
    set "ISCC="
    if exist "C:\Program Files (x86)\Inno Setup 6\ISCC.exe" set "ISCC=C:\Program Files (x86)\Inno Setup 6\ISCC.exe"
    if exist "C:\Program Files\Inno Setup 6\ISCC.exe"       set "ISCC=C:\Program Files\Inno Setup 6\ISCC.exe"

    if not defined ISCC (
        echo [ERRO] Inno Setup 6 nao encontrado.
        echo        Baixe em: https://jrsoftware.org/isdl.php
    ) else (
        echo Compilando instalador...
        "!ISCC!" "installer\setup.iss"
        if %ERRORLEVEL% EQU 0 (
            echo [OK] Instalador criado em .\dist\LAGGADO-Setup-v%VERSION%.exe
        ) else (
            echo [ERRO] Falha ao compilar o instalador.
        )
    )
)

echo.
echo ================================================================
echo  BUILD CONCLUIDO!
echo ================================================================
echo.
echo  Executavel: .\%OUTDIR%\%GUIBIN%
echo.
echo  Para testar: .\%OUTDIR%\%GUIBIN%
echo  Requer execucao como Administrador para WireGuard.
echo.
pause
