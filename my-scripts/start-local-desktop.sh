#!/usr/bin/env bash
# =============================================================================
# start-local-desktop.sh
#
# 编译 Reasonix Desktop 二开版并部署到本地 macOS。
#
# 用法:
#   ./start-local-desktop.sh                    # 完整流程
#   ./start-local-desktop.sh --skip-install     # 跳过依赖安装，仅编译+部署
#   ./start-local-desktop.sh --help             # 显示帮助
#
# 版本号: git describe --tags --always 结果 + "-wkj" 后缀
# 部署位置: /Applications/Reasonix-wkj.app
# =============================================================================

set -euo pipefail

# ---- 路径 ----
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
DESKTOP_DIR="$PROJECT_DIR/desktop"
FRONTEND_DIR="$DESKTOP_DIR/frontend"

# ---- 应用标识 ----
APPNAME="Reasonix-wkj"
BINNAME="reasonix-desktop"

# ---- 颜色 ----
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

log_info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
log_step()  { echo -e "\n${CYAN}━━━ $* ━━━${NC}"; }

# ----- 帮助 -----
if [[ "${1:-}" == "--help" ]]; then
    cat <<EOF
用法: $0 [--skip-install]

选项:
  --skip-install    跳过依赖安装，直接编译 + 部署
  --help            显示此帮助

说明:
  编译产物: /Applications/Reasonix-wkj.app
  版本号:   git describe --tags --always + "-wkj" 后缀
EOF
    exit 0
fi

SKIP_INSTALL=false
[[ "${1:-}" == "--skip-install" ]] && SKIP_INSTALL=true

# =============================================================================
# 阶段 1 — 前置检查
# =============================================================================
check_prerequisites() {
    log_step "检查前置环境"

    command -v go >/dev/null 2>&1 || { log_error "需要 Go，请先安装"; exit 1; }
    log_info "Go: $(go version | grep -oE 'go\S+')"

    command -v node >/dev/null 2>&1 || { log_error "需要 Node.js"; exit 1; }
    log_info "Node.js: $(node --version)"

    if ! command -v pnpm >/dev/null 2>&1; then
        log_warn "未安装 pnpm，正在安装..."
        npm install -g pnpm
    fi
    log_info "pnpm: $(pnpm --version)"

    if ! xcode-select -p >/dev/null 2>&1; then
        log_error "需要 Xcode CLT，请运行: xcode-select --install"; exit 1
    fi
    log_info "Xcode CLT: 已安装"

    # 检查 Wails CLI，缺失则安装
    if ! command -v wails >/dev/null 2>&1; then
        log_info "正在安装 Wails CLI..."
        go install github.com/wailsapp/wails/v2/cmd/wails@latest
        GOPATH_BIN="$(go env GOPATH)/bin"
        if ! echo "$PATH" | grep -q "$GOPATH_BIN"; then
            export PATH="$PATH:$GOPATH_BIN"
        fi
        command -v wails >/dev/null 2>&1 || {
            log_error "Wails CLI 安装失败，请检查 GOPATH/bin 是否在 PATH 中"; exit 1
        }
    fi
    log_info "Wails: $(wails version 2>/dev/null)"

    log_info "环境检查通过"
}

# =============================================================================
# 阶段 2 — 安装前端依赖
# =============================================================================
install_deps() {
    log_step "安装前端依赖"
    [[ -d "$FRONTEND_DIR" ]] || { log_error "前端目录不存在: $FRONTEND_DIR"; exit 1; }

    cd "$FRONTEND_DIR"
    if [[ -d "node_modules" ]]; then
        log_info "node_modules 已存在，跳过安装"
    else
        log_info "执行 pnpm install ..."
        pnpm install
    fi
}

# =============================================================================
# 阶段 3 — 编译 desktop
# =============================================================================
build_desktop() {
    log_step "编译桌面应用"

    cd "$DESKTOP_DIR"
    [[ -d "$FRONTEND_DIR/node_modules" ]] || { log_error "前端依赖未安装"; exit 1; }

    # ---- 解析版本 ----
    RAW_VERSION="$(cd "$PROJECT_DIR" && git describe --tags --always 2>/dev/null || echo "dev")"
    VERSION="${RAW_VERSION}-wkj"

    # 提取纯数字版本用于 CFBundleVersion (macOS 要求 X.Y.Z)
    numver="${RAW_VERSION#desktop-v}"  # desktop-v1.6.0… → 1.6.0…
    numver="${numver#v}"               # v1.2.3 → 1.2.3
    numver="${numver%%-*}"             # 1.6.0-90-xxx → 1.6.0
    [[ "$numver" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || numver="0.0.0"

    log_info "版本:     ${VERSION}"
    log_info "数字版本: ${numver}"

    # ---- 写入 wails.json productVersion (macOS CFBundleVersion) ----
    node -e '
        const fs = require("fs"), f = "wails.json",
              j = JSON.parse(fs.readFileSync(f, "utf8"));
        j.info.productVersion = process.argv[1];
        fs.writeFileSync(f, JSON.stringify(j, null, 2) + "\n");
    ' "$numver"

    # ---- 构建 ----
    # wails build -clean 会自行清理二进制产物，不动源资产
    log_info "执行: wails build -clean -platform darwin/universal ..."
    wails build \
        -clean \
        -platform darwin/universal \
        -ldflags "-X main.version=${VERSION} -X main.channel=stable"

    log_info "编译完成"
}

# =============================================================================
# 阶段 4 — 部署到 /Applications
# =============================================================================
deploy() {
    log_step "部署到 /Applications/"

    src="$DESKTOP_DIR/build/bin/reasonix-desktop.app"
    dst="/Applications/${APPNAME}.app"

    [[ -d "$src" ]] || { log_error "未找到编译产物: $src"; exit 1; }

    # 先清理旧版
    if [[ -d "$dst" ]]; then
        sudo rm -rf "$dst"
    fi

    sudo cp -R "$src" "$dst"
    sudo chown -R root:wheel "$dst"

    # 移除 quarantine（二开版无 Apple 公证）
    if xattr -l "$dst" 2>/dev/null | grep -q com.apple.quarantine; then
        sudo xattr -dr com.apple.quarantine "$dst"
    fi

    log_info "部署完成: $dst"
}

# =============================================================================
# 阶段 5 — 验证
# =============================================================================
verify() {
    log_step "验证安装"

    dst="/Applications/${APPNAME}.app"
    exe="$dst/Contents/MacOS/$BINNAME"

    [[ -d "$dst" ]]  || { log_error "验证失败: $dst 不存在";     exit 1; }
    [[ -f "$exe" ]]  || { log_error "验证失败: 可执行文件缺失";    exit 1; }

    echo "  路径:   $dst"
    echo "  大小:   $(du -sh "$dst" | awk '{print $1}')"
    echo "  可执行: $(ls -lh "$exe" | awk '{print $5}')"

    echo ""
    echo -e "${GREEN}✓ 安装成功${NC}"
    echo ""
    echo -e "  启动: ${CYAN}open ${dst}${NC} 或从 Launchpad 打开"
    echo -e "  卸载: ${YELLOW}sudo rm -rf ${dst}${NC}"
    echo ""
}

# =============================================================================
# 主流程
# =============================================================================

echo ""
echo -e "${CYAN}  ╔═══════════════════════════════════════╗${NC}"
echo -e "${CYAN}  ║   Reasonix Desktop 二开版 本地部署    ║${NC}"
echo -e "${CYAN}  ╚═══════════════════════════════════════╝${NC}"
echo ""

check_prerequisites
$SKIP_INSTALL || install_deps
build_desktop
deploy
verify
