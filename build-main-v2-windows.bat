@echo off
setlocal
cd /d "%~dp0"

where git >nul 2>nul || goto :missing_git
set "BASH_EXE="
for /f "delims=" %%I in ('where bash 2^>nul') do if not defined BASH_EXE set "BASH_EXE=%%I"
if not defined BASH_EXE if exist "%ProgramFiles%\Git\bin\bash.exe" set "BASH_EXE=%ProgramFiles%\Git\bin\bash.exe"
if not defined BASH_EXE if exist "%ProgramFiles(x86)%\Git\bin\bash.exe" set "BASH_EXE=%ProgramFiles(x86)%\Git\bin\bash.exe"
if not defined BASH_EXE goto :missing_bash
where go >nul 2>nul || goto :missing_go
where node >nul 2>nul || goto :missing_node
where pnpm >nul 2>nul || goto :missing_pnpm
where makensis >nul 2>nul || goto :missing_nsis

for /f "delims=" %%B in ('git rev-parse --abbrev-ref HEAD 2^>nul') do set "CURRENT_BRANCH=%%B"
if not "%CURRENT_BRANCH%"=="main-v2" (
    echo Error: this build entry only packages the main-v2 branch.
    echo Current branch: %CURRENT_BRANCH%
    goto :failed
)

echo Building the unsigned Windows x64 desktop client from main-v2...
for /f "delims=" %%D in ('powershell -NoProfile -Command "Get-Date -Format yyyyMMdd"') do set "BUILD_VERSION=v0.0.0-dev-%%D"
echo Version: %BUILD_VERSION%
"%BASH_EXE%" scripts/desktop-build.sh windows/amd64 %BUILD_VERSION% stable
if errorlevel 1 goto :failed

echo.
echo Build complete. Artifacts are in:
echo   %~dp0dist\Reasonix-windows-amd64-installer.exe
echo   %~dp0dist\Reasonix-windows-amd64.zip
goto :done

:missing_git
echo Error: Git is required and must be available on PATH.
goto :failed
:missing_bash
echo Error: Git Bash (bash.exe) is required and must be available on PATH.
goto :failed
:missing_go
echo Error: Go is required and must be available on PATH.
goto :failed
:missing_node
echo Error: Node.js 24 or newer is required and must be available on PATH.
goto :failed
:missing_pnpm
echo Error: pnpm 10 is required and must be available on PATH.
goto :failed
:missing_nsis
echo Error: NSIS (makensis.exe) is required and must be available on PATH.
goto :failed

:failed
echo.
echo Build failed. Review the error above and the build output.
pause
exit /b 1

:done
echo.
pause
exit /b 0
