param(
    [string]$Command = ""
)

$ProjectDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$LocalNode = Join-Path $ProjectDir "nodejs"
$LocalTsx = Join-Path $ProjectDir "node_modules\tsx\dist\cli.mjs"

$env:PATH = "$LocalNode;$ProjectDir\node_modules\.bin;$env:PATH"

Write-Host "[Reasonix] Using local Node.js: $LocalNode" -ForegroundColor Cyan
$nodeVersion = & "$LocalNode\node.exe" --version
Write-Host "[Reasonix] Node.js version: $nodeVersion" -ForegroundColor Cyan

function Show-Menu {
    Write-Host ""
    Write-Host "====== Reasonix Launcher ======" -ForegroundColor Yellow
    Write-Host ""
    Write-Host "  dev    - Start dev mode" -ForegroundColor Green
    Write-Host "  chat   - Start chat mode" -ForegroundColor Green
    Write-Host "  build  - Build project" -ForegroundColor Green
    Write-Host "  npm    - Run local npm" -ForegroundColor Green
    Write-Host "  npx    - Run local npx" -ForegroundColor Green
    Write-Host "  node   - Run local node" -ForegroundColor Green
    Write-Host ""
    Write-Host "Examples:" -ForegroundColor Gray
    Write-Host "  .\start.ps1 dev" -ForegroundColor Gray
    Write-Host "  .\start.ps1 chat" -ForegroundColor Gray
    Write-Host "  .\start.ps1 npm install" -ForegroundColor Gray
    Write-Host "  .\start.ps1 npx ..." -ForegroundColor Gray
    Write-Host ""
}

function Invoke-Dev {
    Write-Host "[Reasonix] Starting dev mode..." -ForegroundColor Green
    Set-Location $ProjectDir
    & "$LocalNode\node.exe" "$LocalTsx" src/cli/index.ts
}

function Invoke-Chat {
    Write-Host "[Reasonix] Starting chat mode..." -ForegroundColor Green
    Set-Location $ProjectDir
    & "$LocalNode\node.exe" "$LocalTsx" src/cli/index.ts chat
}

function Invoke-Build {
    Write-Host "[Reasonix] Building project..." -ForegroundColor Green
    Set-Location $ProjectDir
    & "$LocalNode\npm.cmd" run build
}

function Invoke-Npm {
    Set-Location $ProjectDir
    & "$LocalNode\npm.cmd" $args
}

function Invoke-Npx {
    Set-Location $ProjectDir
    & "$LocalNode\npx.cmd" $args
}

function Invoke-Node {
    Set-Location $ProjectDir
    & "$LocalNode\node.exe" $args
}

switch ($Command.ToLower()) {
    "dev"   { Invoke-Dev }
    "chat"  { Invoke-Chat }
    "build" { Invoke-Build }
    "npm"   { Invoke-Npm $args }
    "npx"   { Invoke-Npx $args }
    "node"  { Invoke-Node $args }
    ""      {
        Show-Menu
        $choice = Read-Host "Enter command (dev/chat/build/npm/npx/node)"
        switch ($choice.ToLower()) {
            "dev"   { Invoke-Dev }
            "chat"  { Invoke-Chat }
            "build" { Invoke-Build }
            "npm"   { Invoke-Npm $args }
            "npx"   { Invoke-Npx $args }
            "node"  { Invoke-Node $args }
            default { Write-Host "Invalid command" -ForegroundColor Red }
        }
    }
    default {
        Write-Host "Unknown command: $Command" -ForegroundColor Red
        Write-Host "Available commands: dev, chat, build, npm, npx, node" -ForegroundColor Yellow
    }
}