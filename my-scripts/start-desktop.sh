#!/usr/bin/env bash
# =============================================================================
# start-desktop.sh — 一键安装依赖并启动 Reasonix Desktop 开发环境
#
# 用法:
#   ./start-desktop.sh                    # 完整安装 + 启动
#   ./start-desktop.sh --skip-install     # 跳过安装，直接启动
#   ./start-desktop.sh --help             # 显示帮助
#
# 前置条件:
#   - Go 1.25+（go.mod 指定 1.25.0）
#   - Node.js 18+（前端构建需要）
#   - pnpm（前端包管理，推荐 8+）
#   - macOS: Xcode Command Line Tools（CGO 需要）
#   - Linux: 需 webkit2gtk 等 Wails 系统依赖
#   - Windows: 需 WebView2（Win10+ 自带）
# =============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
DESKTOP_DIR="$PROJECT_DIR/desktop"
FRONTEND_DIR="$DESKTOP_DIR/frontend"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

log_info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*"; }
log_step()  { echo -e "\n${CYAN}━━━ $* ━━━${NC}"; }

# ----- 帮助 -----
if [[ "${1:-}" == "--help" ]]; then
    echo "用法: $0 [--skip-install]"
    echo ""
    echo "选项:"
    echo "  --skip-install    跳过依赖安装，直接启动 wails dev"
    echo "  --help            显示此帮助"
    exit 0
fi

SKIP_INSTALL=false
if [[ "${1:-}" == "--skip-install" ]]; then
    SKIP_INSTALL=true
fi

# ----- 环境检查 -----
check_prerequisites() {
    log_step "检查前置环境"

    # Go
    if command -v go &>/dev/null; then
        GO_VERSION_RAW=$(go version | grep -oE 'go[0-9]+\.[0-9]+(\.[0-9]+)?' | sed 's/^go//')
        GO_VERSION_MAJOR=$(echo "$GO_VERSION_RAW" | cut -d. -f1)
        GO_VERSION_MINOR=$(echo "$GO_VERSION_RAW" | cut -d. -f2)
        log_info "Go 版本: $(go version | grep -oE 'go version go\S+')"
        if [[ "$GO_VERSION_MAJOR" -gt 1 ]] || { [[ "$GO_VERSION_MAJOR" -eq 1 ]] && [[ "$GO_VERSION_MINOR" -ge 25 ]]; }; then
            :
        else
            log_warn "推荐 Go 1.25+，当前: $GO_VERSION_RAW"
        fi
    else
        log_error "未找到 Go，请先安装 Go 1.25+"
        exit 1
    fi

    # Node.js
    if command -v node &>/dev/null; then
        log_info "Node.js 版本: $(node --version)"
    else
        log_error "未找到 Node.js，请先安装 Node.js 18+"
        exit 1
    fi

    # pnpm
    if command -v pnpm &>/dev/null; then
        log_info "pnpm 版本: $(pnpm --version)"
    else
        log_warn "未找到 pnpm，尝试通过 npm 安装..."
        npm install -g pnpm
    fi

    # macOS: 检查 Xcode Command Line Tools
    if [[ "$(uname)" == "Darwin" ]]; then
        if xcode-select -p &>/dev/null; then
            log_info "Xcode CLT: 已安装"
        else
            log_error "未找到 Xcode Command Line Tools，请运行: xcode-select --install"
            exit 1
        fi
    fi

    # Linux: 检查 webkit2gtk
    if [[ "$(uname)" == "Linux" ]]; then
        if pkg-config --exists webkit2gtk-4.1 2>/dev/null || pkg-config --exists webkit2gtk-4.0 2>/dev/null; then
            log_info "webkit2gtk: 已安装"
        else
            log_warn "未检测到 webkit2gtk，Wails 构建可能失败"
            log_warn "Debian/Ubuntu: sudo apt install libwebkit2gtk-4.1-dev build-essential"
            log_warn "Fedora: sudo dnf install webkit2gtk4.1-devel"
            log_warn "Arch: sudo pacman -S webkit2gtk"
            echo ""
            read -rp "是否继续? [Y/n] " cont
            if [[ "${cont:-Y}" =~ ^[Nn] ]]; then
                exit 1
            fi
        fi
    fi

    log_info "环境检查通过"
}

# ----- 安装依赖 -----
install_dependencies() {
    log_step "安装 Wails CLI"

    # 检查 wails 是否已安装
    if command -v wails &>/dev/null; then
        W_VERSION=$(wails version 2>/dev/null || echo "unknown")
        log_info "Wails CLI 已安装: $W_VERSION"
    else
        log_info "正在安装 Wails CLI..."
        go install github.com/wailsapp/wails/v2/cmd/wails@latest
        log_info "Wails CLI 安装完成"

        # 将 GOPATH/bin 加入 PATH 的提醒
        GOPATH_BIN="$(go env GOPATH)/bin"
        if ! echo "$PATH" | grep -q "$GOPATH_BIN"; then
            log_warn "GOPATH/bin ($GOPATH_BIN) 不在 PATH 中"
            log_warn "请将其加入 PATH，例如: export PATH=\$PATH:$GOPATH_BIN"
            export PATH="$PATH:$GOPATH_BIN"
        fi
    fi

    log_step "安装前端依赖"
    if [[ -d "$FRONTEND_DIR" ]]; then
        cd "$FRONTEND_DIR"
        # 检查 node_modules 是否已存在
        if [[ -d "node_modules" ]]; then
            log_info "node_modules 已存在，检查是否需要更新..."
            # 检查 package.json 和 lockfile 是否有变更
            if [[ -f "node_modules/.package-lock-metadata" ]]; then
                log_info "跳过 pnpm install（使用 --skip-install 跳过全部安装）"
            else
                pnpm install --frozen-lockfile 2>/dev/null || pnpm install
            fi
        else
            log_info "正在安装前端依赖..."
            pnpm install
        fi
    else
        log_error "前端目录不存在: $FRONTEND_DIR"
        exit 1
    fi

    log_info "依赖安装完成"
}

# ----- 打印启动信息 -----
print_launch_info() {
    echo ""
    echo -e "${CYAN}══════════════════════════════════════════════════${NC}"
    echo -e "${CYAN}  启动 Reasonix Desktop 开发环境${NC}"
    echo -e "${CYAN}══════════════════════════════════════════════════${NC}"
    echo ""
    echo -e "  项目目录:    ${GREEN}$PROJECT_DIR${NC}"
    echo -e "  前端目录:    ${GREEN}$FRONTEND_DIR${NC}"
    echo -e "  Wails 模式:  ${YELLOW}dev (热重载)${NC}"
    echo ""
    echo -e "  ${YELLOW}提示:${NC} Wails dev 会同时启动:"
    echo -e "    - Vite 开发服务器 (前端 HMR)"
    echo -e "    - Go 后端 (Wails bridge)"
    echo -e "    - 自动弹出桌面窗口"
    echo ""
    echo -e "  ${YELLOW}提示:${NC} 按 Ctrl+C 停止后，如果需要 Wails 清理，运行:"
    echo -e "    pkill -f 'wails' 2>/dev/null; pkill -f 'reasonix-desktop' 2>/dev/null"
    echo ""
    echo -e "${CYAN}══════════════════════════════════════════════════${NC}"
    echo ""
}

# ----- 启动桌面应用 -----
launch_desktop() {
    log_step "启动 Reasonix Desktop (wails dev)"

    cd "$DESKTOP_DIR"

    if [[ ! -d "$FRONTEND_DIR/node_modules" ]]; then
        log_error "前端依赖未安装，请先运行: cd $FRONTEND_DIR && pnpm install"
        exit 1
    fi

    if ! command -v wails &>/dev/null; then
        log_error "Wails CLI 未安装，请先运行: go install github.com/wailsapp/wails/v2/cmd/wails@latest"
        log_error "或在 PATH 中添加 GOPATH/bin: export PATH=\$PATH:\$(go env GOPATH)/bin"
        exit 1
    fi

    echo ""
    log_info "执行: wails dev (在 $DESKTOP_DIR)"
    echo ""

    # wails dev 会阻塞，所以用 exec 替换当前进程
    exec wails dev
}

# =============================================================================
# 主流程
# =============================================================================

echo ""
echo -e "${CYAN}  ╔═══════════════════════════════════════╗${NC}"
echo -e "${CYAN}  ║   Reasonix Desktop 开发环境启动器    ║${NC}"
echo -e "${CYAN}  ╚═══════════════════════════════════════╝${NC}"
echo ""

check_prerequisites

if [[ "$SKIP_INSTALL" == false ]]; then
    install_dependencies
else
    log_info "跳过依赖安装 (--skip-install)"
fi

print_launch_info
launch_desktop
