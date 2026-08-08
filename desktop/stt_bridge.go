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

// sttStateEvent 是推给前端的识别状态事件名（listening 布尔值），
// 让输入框麦克风按钮与 Edge 识别页实时同步。与 lib/bridge.ts 的
// onSTTState 订阅一致。
const sttStateEvent = "stt:state"

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

	// showPage=false 时 Edge 识别页后台运行（窗口隐藏）。
	showPage bool
	// autoStop 启用"不说话自动停止"；autoStopSeconds 为静默超时（秒）。
	autoStop        bool
	autoStopSeconds int
	// lastSpeechTime 记录最近一次有效语音活动（transcript/status），
	// 自动停止 goroutine 据此判定是否超时。
	lastSpeechTime time.Time
	// stopDone 关闭自动停止监控 goroutine。
	stopDone chan struct{}

	// 全局快捷键（如 "alt+s" / "alt+w"），空串表示禁用。
	hotkeyStart string
	hotkeyStop  string
	// hotkeys 管理全局热键注册（Windows RegisterHotKey；其他平台空实现）。
	hotkeys *sttHotkeyManager
}

// newSTTBridge 创建桥接服务。homeDir 是 Reasonix 用户数据目录（Edge 专用
// profile 的落点）；emit 非空时转录会推给前端，空则仅打印日志（测试用）。
func newSTTBridge(homeDir string, emit func(name string, data ...interface{})) *sttBridge {
	b := &sttBridge{
		emit:            emit,
		lang:            "zh-CN",
		homeDir:         homeDir,
		showPage:        true,
		autoStop:        true,
		autoStopSeconds: 10,
	}
	// 热键回调：开始/停止识别。桥接层自身负责向浏览器发命令。
	// 开始热键按下时服务可能尚未启动（用户还没点过麦克风按钮），
	// StartWithWait 会补一次 Start 并等待浏览器连上后自动开始识别。
	b.hotkeys = newSTTHotkeyManager(
		func() {
			if err := b.StartWithWait(0); err != nil {
				fmt.Printf("[STT] 热键启动识别失败: %v\n", err)
			}
		},
		func() {
			_ = b.StopListening()
		},
	)
	return b
}

// SetOptions 应用设置面板的语音输入配置（识别页显示/自动停止/快捷键）。
// 快捷键变化时重新注册全局热键；识别页显隐变化且服务已运行时立即切换窗口。
func (b *sttBridge) SetOptions(showPage, autoStop bool, autoStopSeconds int, hotkeyStart, hotkeyStop string) {
	b.mu.Lock()
	showPageChanged := b.showPage != showPage
	b.showPage = showPage
	b.autoStop = autoStop
	if autoStopSeconds < 3 {
		autoStopSeconds = 3
	}
	if autoStopSeconds > 300 {
		autoStopSeconds = 300
	}
	b.autoStopSeconds = autoStopSeconds
	hkStartChanged := b.hotkeyStart != hotkeyStart
	hkStopChanged := b.hotkeyStop != hotkeyStop
	b.hotkeyStart = hotkeyStart
	b.hotkeyStop = hotkeyStop
	proc := b.edgeProc
	b.mu.Unlock()

	// 热键注册（不依赖服务是否启动——热键是启动服务的入口）：
	// - 若热键管理器尚未启动（如应用启动时 config 未就绪/从未注册过），
	//   无论值是否变化都注册一次，保证启动后热键可用；
	// - 若已启动且值变化，则重新注册。
	if b.hotkeys != nil {
		hkChanged := hkStartChanged || hkStopChanged
		if !b.hotkeys.isStarted() {
			if err := b.hotkeys.start(hotkeyStart, hotkeyStop); err != nil {
				fmt.Printf("[STT] 全局快捷键注册失败: %v\n", err)
			}
		} else if hkChanged {
			b.hotkeys.stop()
			if err := b.hotkeys.start(hotkeyStart, hotkeyStop); err != nil {
				fmt.Printf("[STT] 全局快捷键重新注册失败: %v\n", err)
			}
		}
	}

	// 识别页显隐变化且 Edge 已启动：立即切换窗口（无需重启服务/Edge）。
	if showPageChanged && proc != nil && proc.Process != nil {
		pid := uint32(proc.Process.Pid)
		if showPage {
			sttShowWindowsForPID(pid)
			sttResizeWindowsForPID(pid, 460, 300)
		} else {
			sttHideWindowsForPID(pid)
		}
	}
}

// Start 启动本地服务并（惰性）拉起 Edge 识别页。幂等：已启动则直接返回。
func (b *sttBridge) Start() error {
	b.mu.Lock()
	if b.server != nil {
		// 服务已在：若浏览器连接丢失（用户手动关了识别页），重新拉起页面。
		if b.browserConn == nil {
			b.mu.Unlock()
			if err := b.launchEdge(); err != nil {
				return err
			}
			return nil
		}
		b.mu.Unlock()
		return nil
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0") // 随机端口，避免固定端口冲突
	if err != nil {
		b.mu.Unlock()
		return fmt.Errorf("stt: listen failed: %w", err)
	}
	b.port = ln.Addr().(*net.TCPAddr).Port

	mux := http.NewServeMux()
	mux.HandleFunc("/", b.handlePage)
	mux.HandleFunc("/ws", b.handleWS)
	b.server = &http.Server{Handler: mux}
	b.mu.Unlock()
	go func() {
		_ = b.server.Serve(ln) // 随应用退出而停止
	}()

	// 拉起 Edge 识别页（首次点击麦克风按钮时才会走到这里）。
	if err := b.launchEdge(); err != nil {
		// 服务已起但 Edge 失败：不致命，前端会收到浏览器未连接状态，
		// 用户可重试。这里只记录，不把服务一起回滚。
		fmt.Printf("[STT] Edge launch failed: %v\n", err)
	}
	// 注册全局快捷键（start/stop）。热键只有在服务启动后才注册——
	// 若设置面板里配置快捷键时服务尚未启动，这里补上注册。
	b.registerHotkeysIfNeeded()
	return nil
}

// registerHotkeysIfNeeded 用当前配置的快捷键注册全局热键（服务已启动时）。
// 注册失败（热键被占用/格式非法）只记录日志，不阻断识别。
func (b *sttBridge) registerHotkeysIfNeeded() {
	b.mu.Lock()
	hotkeyStart := b.hotkeyStart
	hotkeyStop := b.hotkeyStop
	hotkeys := b.hotkeys
	b.mu.Unlock()
	if hotkeys == nil {
		return
	}
	if err := hotkeys.start(hotkeyStart, hotkeyStop); err != nil {
		fmt.Printf("[STT] 全局快捷键注册失败: %v\n", err)
	}
}

// startAutoStopMonitor 启动"不说话自动停止"监控（每个会话只启一次）。
func (b *sttBridge) startAutoStopMonitor() {
	b.mu.Lock()
	if b.stopDone != nil {
		b.mu.Unlock()
		return
	}
	stopDone := make(chan struct{})
	b.stopDone = stopDone
	b.mu.Unlock()

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stopDone:
				return
			case <-ticker.C:
				b.mu.Lock()
				active := b.listening && b.browserConn != nil && b.autoStop
				since := time.Since(b.lastSpeechTime)
				conn := b.browserConn
				b.mu.Unlock()
				if active && since >= time.Duration(b.autoStopSeconds)*time.Second {
					fmt.Printf("[STT] 静默 %v 秒，自动停止识别\n", b.autoStopSeconds)
					if conn != nil {
						_ = conn.WriteJSON(map[string]string{"cmd": "stop"})
					}
					b.mu.Lock()
					b.listening = false
					listening := false
					b.mu.Unlock()
					if b.emit != nil {
						b.emit(sttStateEvent, map[string]any{"listening": listening})
					}
				}
			}
		}
	}()
}

// stopAutoStopMonitor 关闭自动停止监控。
func (b *sttBridge) stopAutoStopMonitor() {
	b.mu.Lock()
	stopDone := b.stopDone
	b.stopDone = nil
	b.mu.Unlock()
	if stopDone != nil {
		close(stopDone)
	}
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

	b.stopAutoStopMonitor()

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
	// proc.Process.Kill() 只杀主进程；Edge 是多进程架构，渲染/GPU 等子进程
	// 会残留不退出。按专用 profile 目录定向清掉所有本 STT 拉起的 msedge 进程
	// （不误杀用户自己的 Edge，edge-stt-bridge 同样做法）。
	killEdgeProfileProcesses(b.homeDir)
}

// killEdgeProfileProcesses 按 CommandLine 匹配本 STT 专用 profile 目录，
// 杀掉所有相关 msedge 进程（含子进程）。非 Windows/失败静默忽略。
func killEdgeProfileProcesses(homeDir string) {
	profileDir := filepath.Join(homeDir, sttEdgeProfileDirName)
	ps := "Get-CimInstance Win32_Process -Filter \"Name='msedge.exe'\" | " +
		"Where-Object { $_.CommandLine -like '*" + profileDir + "*' } | " +
		"ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }"
	_ = exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps).Run()
}

// StartListening 向浏览器发送开始识别命令，并启动自动停止监控。
// 主动推送 listening=true 状态给前端（输入框麦克风按钮高亮），
// 不依赖浏览器回传 status——热键启动时按钮也能即时同步。
func (b *sttBridge) StartListening() error {
	b.mu.Lock()
	if b.browserConn == nil {
		b.mu.Unlock()
		return fmt.Errorf("stt: 浏览器未连接，请重试")
	}
	conn := b.browserConn
	b.mu.Unlock()
	if err := conn.WriteJSON(map[string]string{"cmd": "start", "lang": b.lang}); err != nil {
		return fmt.Errorf("stt: 发送开始命令失败: %w", err)
	}
	b.mu.Lock()
	b.listening = true
	b.lastSpeechTime = time.Now()
	b.mu.Unlock()
	b.startAutoStopMonitor()
	if b.emit != nil {
		b.emit(sttStateEvent, map[string]any{"listening": true})
	}
	return nil
}

// StartWithWait 启动服务（含 Edge 识别页）并等待浏览器 WebSocket 连上后自动
// 发送开始识别命令。首次点击/首次按热键时 Edge 页面加载需要几秒，浏览器尚未
// 连接，直接 StartListening 会报"浏览器未连接"；这里轮询等待连接，一次操作
// 即生效，无需用户再点一次。timeout 为等待上限（0 使用默认 10s）。
func (b *sttBridge) StartWithWait(timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if err := b.Start(); err != nil {
		return err
	}
	// 等待浏览器连接（每 200ms 探测一次）。
	deadline := time.Now().Add(timeout)
	for {
		b.mu.Lock()
		conn := b.browserConn
		b.mu.Unlock()
		if conn != nil {
			return b.StartListening()
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("stt: 等待浏览器连接超时，请重试")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// StopListening 向浏览器发送停止识别命令，并主动推送 listening=false 状态。
func (b *sttBridge) StopListening() error {
	b.mu.Lock()
	conn := b.browserConn
	b.listening = false
	b.mu.Unlock()
	if b.emit != nil {
		b.emit(sttStateEvent, map[string]any{"listening": false})
	}
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
	// 浏览器已连上说明识别页窗口必然已创建：若配置为后台隐藏，这里立即
	// 强制隐藏，杜绝窗口闪现（launchEdge 的定时隐藏是兜底重试）。
	b.mu.Lock()
	hidden := !b.showPage
	proc := b.edgeProc
	b.mu.Unlock()
	if hidden && proc != nil && proc.Process != nil {
		sttHideWindowsForPID(uint32(proc.Process.Pid))
	}

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
			// 有语音活动：重置自动停止计时。
			b.mu.Lock()
			b.lastSpeechTime = time.Now()
			b.mu.Unlock()
			if b.emit != nil {
				b.emit(sttTranscriptEvent, sttTranscriptPayload{Text: text, IsFinal: isFinal})
			} else {
				fmt.Printf("[STT] transcript(final=%v): %s\n", isFinal, text)
			}
		case "status":
			state, _ := msg["state"].(string)
			b.mu.Lock()
			changed := false
			if state == "listening" {
				changed = !b.listening
				b.listening = true
				// 开始识别时重置自动停止计时。
				b.lastSpeechTime = time.Now()
			} else if state == "idle" || state == "error" {
				changed = b.listening
				b.listening = false
			}
			listening := b.listening
			b.mu.Unlock()
			// 识别状态变化时同步给前端（输入框麦克风按钮高亮/复原）。
			if changed && b.emit != nil {
				b.emit(sttStateEvent, map[string]any{"listening": listening})
			}
		}
	}
}

// launchEdge 查找 Edge 并用专用 profile 打开识别页。showPage=false 时把窗口
// 移出屏幕（后台运行），识别照常进行但不打扰用户。
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
	args := []string{
		fmt.Sprintf("--app=%s", url),
		"--auto-accept-this-tab-capture",
		// 自动接受麦克风权限：不弹"使用麦克风"授权提示（仍使用真实设备，
		// 除非同时指定 --use-fake-device-for-media-stream）。
		"--use-fake-ui-for-media-stream",
		"--disable-features=TranslateUI",
		"--disable-extensions",
		fmt.Sprintf("--lang=%s", b.lang),
		fmt.Sprintf("--user-data-dir=%s", profileDir),
	}
	b.mu.Lock()
	hidden := !b.showPage
	b.mu.Unlock()
	if hidden {
		// 后台运行：窗口保持正常位置，用 Win32 ShowWindow(SW_HIDE) 隐藏。
		// 注意不能加 --window-position 把窗口移到屏幕外——Chromium 对屏幕外
		// 窗口判定页面不可见，会暂停 Web Speech 录音/转录（实测"正在识别但
		// 无文字返回"）。edge-stt-bridge 同样只 SW_HIDE 不移动窗口。
		args = append(args, "--window-size=460,300")
	} else {
		// 显示模式：显式设置合适窗口尺寸，保证识别页完整显示
		// （不设尺寸时 Edge --app 可能复用上次隐藏模式的 480×140）。
		args = append(args, "--window-size=460,300")
	}
	cmd := exec.Command(edge, args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 Edge 失败: %w", err)
	}
	b.edgeProc = cmd
	pid := uint32(cmd.Process.Pid)
	fmt.Printf("[STT] Edge 已启动: %s (hidden=%v)\n", url, hidden)
	// 专用 profile 会记住旧窗口位置/尺寸，--window-position/-size 参数会被覆盖，
	// 必须等窗口出现后 Win32 强制隐藏/缩放（edge-stt-bridge 同样做法）。
	// Edge 多进程启动时 splash/中间窗口先出现、主窗口后创建——因此隐藏不能
	// "第一次成功就返回"（主窗口会在 splash 被隐藏后才创建、仍会显示），
	// 必须跑完全部延迟、每轮都隐藏所有匹配窗口，覆盖 ~3 秒启动窗口期。
	go func() {
		delays := []time.Duration{100 * time.Millisecond, 100 * time.Millisecond, 200 * time.Millisecond, 300 * time.Millisecond, 500 * time.Millisecond, 1 * time.Second, 1 * time.Second}
		for i, d := range delays {
			time.Sleep(d)
			if hidden {
				sttHideWindowsForPID(pid)
			} else {
				sttResizeWindowsForPID(pid, 460, 300)
				return
			}
			if i == len(delays)-1 {
				fmt.Printf("[STT] 识别页窗口隐藏轮询完成（共 %d 轮）\n", len(delays))
			}
		}
	}()
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
