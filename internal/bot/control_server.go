package bot

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

const defaultControlAddr = "127.0.0.1:37913"

type controlHTTPServer struct {
	server *http.Server
	addr   string
}

type controlStatusResponse struct {
	Status           string                  `json:"status"`
	ControlAddr      string                  `json:"control_addr,omitempty"`
	ActiveSessions   int                     `json:"active_sessions"`
	RetainedSessions int                     `json:"retained_sessions"`
	StartErrors      []string                `json:"start_errors,omitempty"`
	Adapters         []AdapterHealthSnapshot `json:"adapters"`
}

type controlSendRequest struct {
	ConnectionID string   `json:"connection_id"`
	Domain       string   `json:"domain,omitempty"`
	ChatID       string   `json:"chat_id"`
	ChatType     ChatType `json:"chat_type,omitempty"`
	Text         string   `json:"text,omitempty"`
	MediaURLs    []string `json:"media_urls,omitempty"`
	ReplyToMsgID string   `json:"reply_to_msg_id,omitempty"`
}

func (gw *BotGateway) startControlServer(parent context.Context) error {
	if !gw.cfg.ControlEnabled {
		return nil
	}
	token := strings.TrimSpace(gw.cfg.ControlToken)
	if token == "" {
		return errors.New("bot control is enabled but control token is empty")
	}
	addr := strings.TrimSpace(gw.cfg.ControlAddr)
	if addr == "" {
		addr = defaultControlAddr
	}
	if err := validateLoopbackAddr(addr); err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/status", gw.controlAuth(gw.handleControlStatus))
	mux.HandleFunc("/health", gw.controlAuth(gw.handleControlStatus))
	mux.HandleFunc("/metrics", gw.controlAuth(gw.handleControlMetrics))
	mux.HandleFunc("/send", gw.controlAuth(gw.handleControlSend))

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("start bot control server: %w", err)
	}
	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	control := &controlHTTPServer{server: srv, addr: ln.Addr().String()}
	gw.mu.Lock()
	gw.controlServer = control
	gw.mu.Unlock()

	go func() {
		<-parent.Done()
		gw.stopControlServer()
	}()
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			gw.logger.Warn("bot control server stopped with error", "addr", control.addr, "err", err)
		}
	}()
	gw.logger.Info("bot control server started", "addr", control.addr)
	return nil
}

func (gw *BotGateway) stopControlServer() {
	gw.mu.Lock()
	control := gw.controlServer
	gw.controlServer = nil
	gw.mu.Unlock()
	if control == nil || control.server == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := control.server.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		gw.logger.Warn("bot control server shutdown failed", "addr", control.addr, "err", err)
	}
}

// ControlAddr returns the bound loopback control address when enabled.
func (gw *BotGateway) ControlAddr() string {
	gw.mu.Lock()
	defer gw.mu.Unlock()
	if gw.controlServer == nil {
		return ""
	}
	return gw.controlServer.addr
}

func validateLoopbackAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("bot control addr must be host:port on loopback: %w", err)
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("bot control addr must bind to loopback, got %q", addr)
	}
	return nil
}

func (gw *BotGateway) controlAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimSpace(r.Header.Get("Authorization"))
		const prefix = "Bearer "
		if !strings.HasPrefix(got, prefix) || subtle.ConstantTimeCompare([]byte(strings.TrimSpace(strings.TrimPrefix(got, prefix))), []byte(strings.TrimSpace(gw.cfg.ControlToken))) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (gw *BotGateway) handleControlStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	gw.mu.Lock()
	retained := len(gw.controllers)
	gw.mu.Unlock()
	startErrs := gw.StartErrors()
	errTexts := make([]string, 0, len(startErrs))
	for _, err := range startErrs {
		if err != nil {
			errTexts = append(errTexts, err.Error())
		}
	}
	status := "running"
	for _, health := range gw.AdapterHealth() {
		switch health.Status {
		case "error", "degraded", "closed":
			status = "degraded"
		}
	}
	writeControlJSON(w, controlStatusResponse{
		Status:           status,
		ControlAddr:      gw.ControlAddr(),
		ActiveSessions:   gw.sessions.ActiveCount(),
		RetainedSessions: retained,
		StartErrors:      errTexts,
		Adapters:         gw.AdapterHealth(),
	})
}

func (gw *BotGateway) handleControlSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req controlSendRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.ConnectionID) == "" || strings.TrimSpace(req.ChatID) == "" {
		http.Error(w, "connection_id and chat_id are required", http.StatusBadRequest)
		return
	}
	result, err := gw.SendToAdapter(r.Context(), req.ConnectionID, req.Domain, OutboundMessage{
		ConnectionID: req.ConnectionID,
		Domain:       req.Domain,
		ChatID:       req.ChatID,
		ChatType:     req.ChatType,
		Text:         req.Text,
		MediaURLs:    req.MediaURLs,
		ReplyToMsgID: req.ReplyToMsgID,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeControlJSON(w, result)
}

func (gw *BotGateway) handleControlMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	gw.mu.Lock()
	retained := len(gw.controllers)
	gw.mu.Unlock()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "# TYPE reasonix_bot_active_sessions gauge\nreasonix_bot_active_sessions %d\n", gw.sessions.ActiveCount())
	fmt.Fprintf(w, "# TYPE reasonix_bot_retained_sessions gauge\nreasonix_bot_retained_sessions %d\n", retained)
	fmt.Fprintln(w, "# TYPE reasonix_bot_adapter_messages_total counter")
	for _, health := range gw.AdapterHealth() {
		labels := adapterMetricLabels(health)
		fmt.Fprintf(w, "reasonix_bot_adapter_messages_total{%s} %d\n", labels, health.Messages)
	}
	fmt.Fprintln(w, "# TYPE reasonix_bot_adapter_sends_total counter")
	for _, health := range gw.AdapterHealth() {
		labels := adapterMetricLabels(health)
		fmt.Fprintf(w, "reasonix_bot_adapter_sends_total{%s} %d\n", labels, health.Sends)
	}
	fmt.Fprintln(w, "# TYPE reasonix_bot_adapter_send_errors_total counter")
	for _, health := range gw.AdapterHealth() {
		labels := adapterMetricLabels(health)
		fmt.Fprintf(w, "reasonix_bot_adapter_send_errors_total{%s} %d\n", labels, health.SendErrors)
	}
	fmt.Fprintln(w, "# TYPE reasonix_bot_adapter_status gauge")
	for _, health := range gw.AdapterHealth() {
		labels := adapterMetricLabels(health)
		fmt.Fprintf(w, "reasonix_bot_adapter_status{%s,status=\"%s\"} 1\n", labels, prometheusLabelValue(health.Status))
	}
}

func writeControlJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func adapterMetricLabels(health AdapterHealthSnapshot) string {
	return fmt.Sprintf("id=\"%s\",platform=\"%s\",domain=\"%s\"",
		prometheusLabelValue(health.ID),
		prometheusLabelValue(string(health.Platform)),
		prometheusLabelValue(health.Domain),
	)
}

func prometheusLabelValue(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\n", "\\n")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	return value
}
