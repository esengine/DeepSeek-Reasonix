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

wails dev -tags native_webview2loader

echo.
echo wails dev exited. Press any key to close this window.
pause >nul
