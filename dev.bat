@echo off
REM ============================================================
REM  Reasonix desktop one-click dev launcher
REM  Double-click: cd to repo root -> set env -> run wails dev
REM  NOTE: keep this file ASCII-only. cmd.exe parses batch files
REM  with the OEM/GBK codepage, so non-ASCII comments garble and
REM  get executed as commands (e.g. "M is not recognized...").
REM ============================================================
setlocal

REM cd to the repo root (%~dp0 is this bat's directory)
cd /d "%~dp0"

REM ---- local tool paths (Go / wails / gcc) ----
set "GOROOT=D:\Tools\go-install\go"
set "PATH=D:\Tools\go\bin;D:\Tools\mingw64\bin;D:\Tools\go-install\go\bin;%PATH%"

REM bypass the official single-instance lock so dev builds run standalone
set "REASONIX_DEV=1"

REM ---- cleanup any previous dev instance to free port 5173 ----
REM (two wails dev runs conflict on the Vite port; kill old dev ones first)
REM NOTE: do NOT kill reasonix-desktop.exe (installed release build) - the user
REM may run the release app and dev mode side by side. Only the -dev process,
REM the Vite port owner and stale wails dev processes are cleaned up here.
taskkill /F /IM reasonix-desktop-dev.exe >nul 2>&1
for /f "tokens=5" %%a in ('netstat -ano ^| findstr ":5173" ^| findstr "LISTENING"') do taskkill /F /PID %%a >nul 2>&1
powershell -NoProfile -Command "Get-CimInstance Win32_Process | Where-Object { $_.CommandLine -match 'wails\s+dev' } | ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }" >nul 2>&1

echo ============================================================
echo  Reasonix Desktop - dev mode
echo  GOROOT=%GOROOT%
echo  REASONIX_DEV=%REASONIX_DEV%
echo ============================================================
echo.
echo Starting wails dev (first build takes a few minutes)...
echo The app window means the source desktop app is running.
echo Press Ctrl+C in this window to stop.
echo.
cd desktop

wails dev

echo.
echo wails dev exited. Press any key to close this window.
pause >nul
