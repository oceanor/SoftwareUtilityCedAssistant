@echo off
setlocal
cd /d "%~dp0"

for /f "delims=" %%A in ('go env GOARCH') do set "GOHOSTARCH=%%A"

rem I test girano senza rsrc.syso: il manifest "requireAdministrator" impedirebbe di avviare l'exe di test.
if exist rsrc.syso del /q rsrc.syso

echo Test...
go test ./...
if %errorlevel% neq 0 (
  echo ERRORE: test falliti.
  pause
  exit /b 1
)

echo Rigenerazione risorse Windows (main.manifest + icon.ico)...
go run github.com/akavel/rsrc@v0.10.2 -manifest main.manifest -ico icon.ico -arch %GOHOSTARCH% -o rsrc.syso
if %errorlevel% neq 0 (
  echo ERRORE: impossibile generare rsrc.syso ^(rsrc^). Verifica connessione Internet e Go.
  pause
  exit /b 1
)

echo Compilazione in corso (sovrascrittura SoftwareUtilityCedAssistant.exe^)...
go build -ldflags="-H windowsgui" -o SoftwareUtilityCedAssistant.exe .
if %errorlevel% neq 0 (
  echo ERRORE: compilazione fallita.
  pause
  exit /b 1
)
echo Completato: SoftwareUtilityCedAssistant.exe
pause
