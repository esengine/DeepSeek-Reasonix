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
ok()    { echo -e "\033[32m[PASS]\033[0m $1"; ((PASS++)); }
fail()  { echo -e "\033[31m[FAIL]\033[0m $1"; ((FAIL++)); FAILURES+="  ❌ $1\n"; }

# ── 1. 检查端口 ──
info "🔍 检查 Reasonix Relay 是否在运行..."
if ! lsof -i :${RELAY_PORT} &>/dev/null; then
  echo "❌ Reasonix Relay 未运行在端口 ${RELAY_PORT}。请先启动 Reasonix。"
  exit 1
fi
ok "Reasonix Relay 正在运行 (端口 ${RELAY_PORT})"

# ── 2. 检查工具注册 ──
info "🔍 检查所有工具是否注册..."
TOOLS=$(reasonix tools list 2>/dev/null || echo "cannot list tools")
echo "$TOOLS" | grep -q "browser_status" && ok "browser_status" || fail "browser_status"
echo "$TOOLS" | grep -q "browser_navigate" && ok "browser_navigate" || fail "browser_navigate"
echo "$TOOLS" | grep -q "browser_click" && ok "browser_click" || fail "browser_click"
echo "$TOOLS" | grep -q "browser_type" && ok "browser_type" || fail "browser_type"
echo "$TOOLS" | grep -q "browser_read" && ok "browser_read" || fail "browser_read"
echo "$TOOLS" | grep -q "browser_screenshot" && ok "browser_screenshot" || fail "browser_screenshot"
echo "$TOOLS" | grep -q "browser_eval" && ok "browser_eval" || fail "browser_eval"
echo "$TOOLS" | grep -q "browser_list_pages" && ok "browser_list_pages" || fail "browser_list_pages"
echo "$TOOLS" | grep -q "browser_select_page" && ok "browser_select_page" || fail "browser_select_page"
echo "$TOOLS" | grep -q "browser_new_page" && ok "browser_new_page" || fail "browser_new_page"
echo "$TOOLS" | grep -q "browser_close_page" && ok "browser_close_page" || fail "browser_close_page"
echo "$TOOLS" | grep -q "browser_read_dom" && ok "browser_read_dom" || fail "browser_read_dom"
echo "$TOOLS" | grep -q "browser_scroll" && ok "browser_scroll" || fail "browser_scroll"
echo "$TOOLS" | grep -q "browser_go_back" && ok "browser_go_back" || fail "browser_go_back"
echo "$TOOLS" | grep -q "browser_go_forward" && ok "browser_go_forward" || fail "browser_go_forward"
echo "$TOOLS" | grep -q "browser_press_key" && ok "browser_press_key" || fail "browser_press_key"
echo "$TOOLS" | grep -q "browser_hover" && ok "browser_hover" || fail "browser_hover"
echo "$TOOLS" | grep -q "browser_wait" && ok "browser_wait" || fail "browser_wait"
echo "$TOOLS" | grep -q "browser_upload_file" && ok "browser_upload_file" || fail "browser_upload_file"
echo "$TOOLS" | grep -q "browser_resize" && ok "browser_resize" || fail "browser_resize"
echo "$TOOLS" | grep -q "browser_handle_dialog" && ok "browser_handle_dialog" || fail "browser_handle_dialog"
echo "$TOOLS" | grep -q "browser_fill_form" && ok "browser_fill_form" || fail "browser_fill_form"
echo "$TOOLS" | grep -q "browser_attached_pages" && ok "browser_attached_pages" || fail "browser_attached_pages"

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