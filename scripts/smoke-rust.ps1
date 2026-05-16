$ErrorActionPreference = "Stop"
$worktree = Split-Path $PSScriptRoot -Parent
Set-Location $worktree

$bin = Join-Path $worktree "target/release/reasonix-render.exe"
# Always run cargo build — it's a no-op when source hasn't changed but ensures
# the binary on disk matches the worktree's source state, not whatever was
# built last time.
Write-Host "Building release binary (incremental, no-op if no source change)..." -ForegroundColor Gray
cargo build --release --bin reasonix-render
if ($LASTEXITCODE -ne 0) { throw "cargo build (release) failed — check if a reasonix session is still running and holding the .exe locked" }

$env:REASONIX_RENDERER = "rust"
$env:REASONIX_RENDER_CMD = ConvertTo-Json -Compress @($bin)
$env:REASONIX_INPUT_CMD = ConvertTo-Json -Compress @($bin, "--emit-input")

Write-Host ""
Write-Host "=== REASONIX_RENDERER=rust smoke test ===" -ForegroundColor Cyan
Write-Host "Renderer binary : $bin" -ForegroundColor Gray
Write-Host "Input child     : $bin --emit-input (skips cargo overhead)" -ForegroundColor Gray
Write-Host ""
Write-Host "Launching now..." -ForegroundColor Green
Write-Host ""

npm run chat
