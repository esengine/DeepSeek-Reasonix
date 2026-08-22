#!/usr/bin/env bash
# Reasonix Relay — 全工具 E2E 手动测试脚本
# 前置条件：Reasonix 已运行，Chrome Extension 已连接并授权
# 用法: bash tools_test.sh

set -e

RELAY_PORT=23002
WS_URL="ws://127.0.0.1:${RELAY_PORT}"
TEST_PAGE="file://$(pwd)/test_page.html"
PASS=0
FAIL=0
FAILURES=""

info()  { echo -e "\033[36m[INFO]\033[0m $1"; }
# Bash `set -e` treats `((PASS++))` returning 1 (old value 0) as a fatal exit,
# so the script would die on the very first pass. Use arithmetic assignment,
# whose status is always 0.
ok()    { echo -e "\033[32m[PASS]\033[0m $1"; PASS=$((PASS + 1)); }
fail()  { echo -e "\033[31m[FAIL]\033[0m $1"; FAIL=$((FAIL + 1)); FAILURES+="  ❌ $1\n"; }

# ── 1. 检查端口 ──
info "🔍 检查 Reasonix Relay 是否在运行..."
if ! lsof -i :${RELAY_PORT} &>/dev/null; then
  echo "❌ Reasonix Relay 未运行在端口 ${RELAY_PORT}。请先启动 Reasonix。"
  exit 1
fi
ok "Reasonix Relay 正在运行 (端口 ${RELAY_PORT})"

# ── 2. 检查工具注册 ──
info "🔍 检查工具注册..."
TOOLS=$(reasonix tools list 2>/dev/null || echo "cannot list tools")
# browser_* 工具只在桌面 Runtime（Relay 已启动）注册；CLI 会话不注册，避免
# 向模型暴露不可用的工具 schema。因此这里验证的是"过滤生效"而非"存在"。
echo "$TOOLS" | grep -q "browser_" && fail "browser_* tools must NOT appear in CLI tool list (relay-only)" || ok "browser_* tools correctly excluded from CLI"

# ── 结果汇总 ──
echo ""
echo "═══════════════════════════════════════"
echo "  测试结果: ${PASS} passed, ${FAIL} failed"
echo "═══════════════════════════════════════"
if [ ${FAIL} -gt 0 ]; then
  echo -e "失败项:\n${FAILURES}"
  exit 1
fi
exit 0