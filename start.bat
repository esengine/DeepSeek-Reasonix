@echo off
setlocal

set "PROJECT_DIR=%~dp0"
set "LOCAL_NODE=%PROJECT_DIR%nodejs"
set "LOCAL_TSX=%PROJECT_DIR%node_modules\tsx\dist\cli.mjs"

set "PATH=%LOCAL_NODE%;%PROJECT_DIR%node_modules\.bin;%PATH%"

echo [Reasonix] Using local Node.js: %LOCAL_NODE%
"%LOCAL_NODE%\node.exe" --version

if "%1"=="" goto :menu

if /i "%1"=="dev" goto :dev
if /i "%1"=="chat" goto :chat
if /i "%1"=="build" goto :build
if /i "%1"=="npm" goto :npm
if /i "%1"=="npx" goto :npx
if /i "%1"=="node" goto :node

:menu
echo.
echo ====== Reasonix Launcher ======
echo.
echo   dev    - Start dev mode
echo   chat   - Start chat mode
echo   build  - Build project
echo   npm    - Run local npm
echo   npx    - Run local npx
echo   node   - Run local node
echo.
echo Examples:
echo   start.bat dev
echo   start.bat chat
echo   start.bat npm install
echo   start.bat npx tsx src/cli/index.ts
echo.
set /p CMD="Enter command (dev/chat/build/npm/npx/node): "
if /i "%CMD%"=="dev" goto :dev
if /i "%CMD%"=="chat" goto :chat
if /i "%CMD%"=="build" goto :build
if /i "%CMD%"=="npm" goto :npm
if /i "%CMD%"=="npx" goto :npx
if /i "%CMD%"=="node" goto :node
goto :eof

:dev
echo.
echo [Reasonix] Starting dev mode...
cd /d "%PROJECT_DIR%"
"%LOCAL_NODE%\node.exe" "%LOCAL_TSX%" src/cli/index.ts
goto :eof

:chat
echo.
echo [Reasonix] Starting chat mode...
cd /d "%PROJECT_DIR%"
"%LOCAL_NODE%\node.exe" "%LOCAL_TSX%" src/cli/index.ts chat
goto :eof

:build
echo.
echo [Reasonix] Building project...
cd /d "%PROJECT_DIR%"
"%LOCAL_NODE%\npm.cmd" run build
goto :eof

:npm
echo.
if not "%2"=="" echo [Reasonix] Running: npm %2 %3 %4 %5 %6 %7 %8 %9
cd /d "%PROJECT_DIR%"
"%LOCAL_NODE%\npm.cmd" %2 %3 %4 %5 %6 %7 %8 %9
goto :eof

:npx
echo.
if not "%2"=="" echo [Reasonix] Running: npx %2 %3 %4 %5 %6 %7 %8 %9
cd /d "%PROJECT_DIR%"
"%LOCAL_NODE%\npx.cmd" %2 %3 %4 %5 %6 %7 %8 %9
goto :eof

:node
echo.
if not "%2"=="" echo [Reasonix] Running: node %2 %3 %4 %5 %6 %7 %8 %9
cd /d "%PROJECT_DIR%"
"%LOCAL_NODE%\node.exe" %2 %3 %4 %5 %6 %7 %8 %9
goto :eof

endlocal