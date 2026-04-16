@echo off
:: ============================================================
::  LAGGADO — Compila o binario Linux para VPS (laggado-lagger)
::  Nao precisa de GCC — CGO_ENABLED=0, binario puro Go
::  Uso: execute na pasta raiz do projeto
:: ============================================================

setlocal

set GOOS=linux
set GOARCH=amd64
set CGO_ENABLED=0
set VERSION=0.2.0
set OUTFILE=dist\laggado-lagger

if not exist dist mkdir dist

echo.
echo Compilando laggado-lagger para Linux (amd64)...
echo.

go build -ldflags="-s -w -X main.version=%VERSION%" -o "%OUTFILE%" .\cmd\laggado-lagger

if %ERRORLEVEL% NEQ 0 (
    echo [ERRO] Falha ao compilar.
    pause
    exit /b 1
)

echo [OK] Binario gerado: %OUTFILE%
echo.
echo Proximos passos:
echo   1. Envie para o VPS:
echo      scp dist\laggado-lagger root@^<IP_DO_VPS^>:/opt/laggado-lagger/
echo      scp vps-setup.sh root@^<IP_DO_VPS^>:~/
echo.
echo   2. No VPS (Ubuntu/Debian):
echo      sudo bash vps-setup.sh SA "Sao Paulo" BR
echo.
echo   3. Abra no painel do VPS:
echo      UDP 51820  -- WireGuard
echo      TCP 7735   -- Relay HTTP API
echo.
pause
