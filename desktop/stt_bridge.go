package main

// STT（语音转文字）桥接服务：桌面输入框麦克风按钮的后端。
//
// 架构与 edge-stt-bridge 保持一致，但用 Go 内嵌实现、不依赖 Python：
//
//	桌面输入框[麦克风按钮] ──绑定调用──► 本服务(127.0.0.1:随机端口)
//	       ▲                                    │ WebSocket(/ws)
//	       │ runtime.EventsEmit("stt:transcript") │
//	       └────────────── Go ◄──── Edge 识别页(stt_page.html, Web Speech API)
//
// 协议（与源项目一致）：
//   - Server → Browser: {"cmd":"start","lang":"zh-CN"} / {"cmd":"stop"}
//     / {"cmd":"setLang","lang":...}
//   - Browser → Server: {"type":"transcript","text":"...","isFinal":true,...}
//     / {"type":"status","state":"listening"|"idle"|"error",...} / {"type":"connected"}
//
// 已知坑（沿用源项目 AGENTS.md）：
//   - WebSocket /ws 是单客户端：新连接覆盖旧连接。
//   - 本机 urllib/HTTP 调用必须用 127.0.0.1 而非 localhost（IPv6 回退慢）。
//   - Web Speech API 需联网（音频发送到微软服务器处理）。
//   - 首次使用需在 Edge 中手动授权麦克风，页面会引导。
//   - Edge 用专用 user-data-dir（应用数据目录下），避免影响用户日常浏览器。

import (
	"context"
	"embed"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"reasonix/internal/config"
)

//go:embed stt_page.html
var sttPageFS embed.FS

// sttPageName 是嵌入的识别页文件名（go:embed 目录内的相对名）。
const sttPageName = "stt_page.html"

// sttEdgeProfileDirName 是 Edge 专用 profile 目录名（放在 Reasonix home 下）。
const sttEdgeProfileDirName = "edge-stt-profile"

// sttTranscriptEvent 是推给前端的转录事件名，与 lib/bridge.ts 的
// onSTTTranscript 订阅一致。
const sttTranscriptEvent = "stt:transcript"

// sttDefaultEdgePaths 是 Windows 上 Edge 的常见安装位置（按顺序探测）。
var sttDefaultEdgePaths = []string{
	`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
	`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
}

// sttTranscriptPayload 是每次转录推给前端的负载。isFinal=true 时前端把文本
// 插入输入框光标处；isFinal=false 是中间结果，供状态展示（可选）。
type sttTranscriptPayload struct {
	Text    string `json:"text"`
	IsFinal bool   `json:"isFinal"`
	Error   string `json:"error,omitempty"`
}

// sttBridge 管理一次桌面 STT 会话：本地 HTTP+WebSocket 服务、Edge 识别页进程、
// 浏览器连接状态，并把转录转发给前端。
type sttBridge struct {
	mu sync.Mutex

	// emit 把转录事件推给 WebView（由 App 注入 runtime.EventsEmit 的包装）。
	emit func(name string, data ...interface{})

	server *http.Server
	port   int

	// browserConn 是 Edge 识别页的 WebSocket 连接（单客户端）。
	browserConn *websocket.Conn
	listening   bool
	lang        string

	edgeProc *exec.Cmd
	homeDir  string
}

// newSTTBridge 创建桥接服务。homeDir 是 Reasonix 用户数据目录（Edge 专用
// profile 的落点）；emit 非空时转录会推给前端，空则仅打印日志（测试用）。
func newSTTBridge(homeDir string, emit func(name string, data ...interface{})) *sttBridge {
	return &sttBridge{
		emit:    emit,
		lang:    "zh-CN",
		homeDir: homeDir,
	}
}

// Start 启动本地服务并（惰性）拉起 Edge 识别页。幂等：已启动则直接返回。
func (b *sttBridge) Start() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.server != nil {
		return nil
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0") // 随机端口，避免固定端口冲突
	if err != nil {
		return fmt.Errorf("stt: listen failed: %w", err)
	}
	b.port = ln.Addr().(*net.TCPAddr).Port

	mux := http.NewServeMux()
	mux.HandleFunc("/", b.handlePage)
	mux.HandleFunc("/ws", b.handleWS)
	b.server = &http.Server{Handler: mux}
	go func() {
		_ = b.server.Serve(ln) // 随应用退出而停止
	}()

	// 拉起 Edge 识别页（首次点击麦克风按钮时才会走到这里）。
	if err := b.launchEdge(); err != nil {
		// 服务已起但 Edge 失败：不致命，前端会收到浏览器未连接状态，
		// 用户可重试。这里只记录，不把服务一起回滚。
		fmt.Printf("[STT] Edge launch failed: %v\n", err)
	}
	return nil
}

// Stop 停止识别并关闭本地服务。应用退出/设置关闭时调用。
func (b *sttBridge) Stop() {
	b.mu.Lock()
	server := b.server
	conn := b.browserConn
	proc := b.edgeProc
	b.server = nil
	b.browserConn = nil
	b.listening = false
	b.edgeProc = nil
	b.mu.Unlock()

	if conn != nil {
		_ = conn.WriteJSON(map[string]string{"cmd": "stop"})
		_ = conn.Close()
	}
	if server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}
	if proc != nil && proc.Process != nil {
		_ = proc.Process.Kill()
		_, _ = proc.Process.Wait()
	}
}

// StartListening 向浏览器发送开始识别命令。
func (b *sttBridge) StartListening() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.browserConn == nil {
		return fmt.Errorf("stt: 浏览器未连接，请重试")
	}
	if err := b.browserConn.WriteJSON(map[string]string{"cmd": "start", "lang": b.lang}); err != nil {
		return fmt.Errorf("stt: 发送开始命令失败: %w", err)
	}
	b.listening = true
	return nil
}

// StopListening 向浏览器发送停止识别命令。
func (b *sttBridge) StopListening() error {
	b.mu.Lock()
	conn := b.browserConn
	b.listening = false
	b.mu.Unlock()
	if conn == nil {
		return nil
	}
	return conn.WriteJSON(map[string]string{"cmd": "stop"})
}

// SetLang 切换识别语言（如 zh-CN / en-US），正在识别时浏览器端会重启识别。
func (b *sttBridge) SetLang(lang string) error {
	if lang == "" {
		lang = "zh-CN"
	}
	b.mu.Lock()
	b.lang = lang
	conn := b.browserConn
	b.mu.Unlock()
	if conn == nil {
		return nil
	}
	return conn.WriteJSON(map[string]string{"cmd": "setLang", "lang": lang})
}

// Status 返回当前状态（供前端按钮状态/诊断展示）。
func (b *sttBridge) Status() map[string]any {
	b.mu.Lock()
	defer b.mu.Unlock()
	return map[string]any{
		"running":   b.server != nil,
		"listening": b.listening,
		"connected": b.browserConn != nil,
		"lang":      b.lang,
		"port":      b.port,
	}
}

// handlePage 返回嵌入的识别页。
func (b *sttBridge) handlePage(w http.ResponseWriter, r *http.Request) {
	data, err := sttPageFS.ReadFile(sttPageName)
	if err != nil {
		http.Error(w, "stt page missing", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

// upgrader 升级 /ws 为 WebSocket。识别页来自本机 Edge，放开 Origin 校验。
var sttUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// handleWS 接收 Edge 识别页的 WebSocket 连接（单客户端）。
func (b *sttBridge) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := sttUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	b.mu.Lock()
	prev := b.browserConn
	b.browserConn = conn
	b.mu.Unlock()
	if prev != nil {
		_ = prev.Close() // 单客户端：新连接覆盖旧连接
	}
	fmt.Println("[STT] Edge 识别页已连接")

	defer func() {
		b.mu.Lock()
		if b.browserConn == conn {
			b.browserConn = nil
			b.listening = false
		}
		b.mu.Unlock()
		_ = conn.Close()
		fmt.Println("[STT] Edge 识别页断开")
	}()

	for {
		var msg map[string]any
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}
		typ, _ := msg["type"].(string)
		switch typ {
		case "transcript":
			text, _ := msg["text"].(string)
			isFinal, _ := msg["isFinal"].(bool)
			if isFinal && strings.TrimSpace(text) == "" {
				continue
			}
			if b.emit != nil {
				b.emit(sttTranscriptEvent, sttTranscriptPayload{Text: text, IsFinal: isFinal})
			} else {
				fmt.Printf("[STT] transcript(final=%v): %s\n", isFinal, text)
			}
		case "status":
			state, _ := msg["state"].(string)
			b.mu.Lock()
			if state == "listening" {
				b.listening = true
			} else if state == "idle" || state == "error" {
				b.listening = false
			}
			b.mu.Unlock()
		}
	}
}

// launchEdge 查找 Edge 并用专用 profile 打开识别页。
func (b *sttBridge) launchEdge() error {
	edge := b.findEdge()
	if edge == "" {
		return fmt.Errorf("未找到 Microsoft Edge，请确认已安装（或设置 EDGE_PATH 环境变量）")
	}
	profileDir := filepath.Join(b.homeDir, sttEdgeProfileDirName)
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		return fmt.Errorf("创建 Edge profile 目录失败: %w", err)
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/", b.port)
	cmd := exec.Command(edge,
		fmt.Sprintf("--app=%s", url),
		"--auto-accept-this-tab-capture",
		"--disable-features=TranslateUI",
		"--disable-extensions",
		fmt.Sprintf("--lang=%s", b.lang),
		fmt.Sprintf("--user-data-dir=%s", profileDir),
	)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 Edge 失败: %w", err)
	}
	b.edgeProc = cmd
	fmt.Printf("[STT] Edge 已启动: %s\n", url)
	return nil
}

// findEdge 返回 Edge 可执行文件路径。优先 EDGE_PATH 环境变量，其次常见安装
// 位置；找不到返回空串。
func (b *sttBridge) findEdge() string {
	if p := strings.TrimSpace(os.Getenv("EDGE_PATH")); p != "" {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	for _, p := range sttDefaultEdgePaths {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	// 兜底：解析 Windows 注册表安装路径（Key 为空表示默认的 stable 通道）。
	if p := sttEdgeFromRegistry(); p != "" {
		return p
	}
	return ""
}

// sttEdgeFromRegistry 从 Windows 注册表读取 Edge 安装路径（非 Windows 返回空）。
func sttEdgeFromRegistry() string {
	if os.Getenv("GOOS") == "windows" {
		// 走 PowerShell 查询，避免依赖额外注册表库。
		out, err := exec.Command("powershell", "-NoProfile", "-Command",
			`(Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\msedge.exe' -ErrorAction SilentlyContinue).'(Default)'`).Output()
		if err == nil {
			p := strings.TrimSpace(string(out))
			if p != "" {
				if st, err := os.Stat(p); err == nil && !st.IsDir() {
					return p
				}
			}
		}
	}
	return ""
}

// sttEdgeLaunched reports whether an Edge process was started by this bridge
// (used by tests / cleanup paths that must not kill the user's own Edge).
func (b *sttBridge) sttEdgeLaunched() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.edgeProc != nil
}

// silenceUnusedConfigImport keeps the config import referenced on platforms or
// build tags where launchEdge is compiled out (defensive; normally unused here).
var _ = config.ReasonixHomeDir
