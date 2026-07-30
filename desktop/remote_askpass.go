package main

import (
	"bufio"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	remoteAskPassProtocolVersion = 1
	remoteAskPassMaxPromptBytes  = 16 << 10
	remoteAskPassMaxAnswerBytes  = 16 << 10
	remoteAskPassMaxFrameBytes   = 64 << 10

	remoteAskPassModeEnv     = "REASONIX_REMOTE_ASKPASS"
	remoteAskPassEndpointEnv = "REASONIX_REMOTE_ASKPASS_ENDPOINT"
	remoteAskPassKeyEnv      = "REASONIX_REMOTE_ASKPASS_KEY"
	remoteAskPassDeadlineEnv = "REASONIX_REMOTE_ASKPASS_DEADLINE_UNIX_MS"
)

type RemoteAskPassPromptKind string

const (
	RemoteAskPassHostKeyConfirm RemoteAskPassPromptKind = "host_key_confirm"
	RemoteAskPassHostKeyChanged RemoteAskPassPromptKind = "host_key_changed"
	RemoteAskPassPassword       RemoteAskPassPromptKind = "password"
	RemoteAskPassKeyPassphrase  RemoteAskPassPromptKind = "key_passphrase"
	RemoteAskPassVerification   RemoteAskPassPromptKind = "verification_code"
	RemoteAskPassAuthentication RemoteAskPassPromptKind = "authentication"
)

type RemoteAskPassPrompt struct {
	Kind      RemoteAskPassPromptKind
	Message   string
	HostLabel string
}

// RemoteAskPassAnswer is returned by the Desktop UI callback. Accepted must be
// explicit: a zero-value answer rejects the prompt and can never approve a Host
// key accidentally.
type RemoteAskPassAnswer struct {
	Accepted bool
	Value    string
}

type RemoteAskPassHandler func(context.Context, RemoteAskPassPrompt) (RemoteAskPassAnswer, error)

// RemoteAskPassBroker is an ephemeral loopback capability. Its 256-bit session
// key and every 256-bit one-time request token exist only in process memory (and
// the inherited helper environment); no credential is written to disk.
type RemoteAskPassBroker struct {
	listener net.Listener
	endpoint string
	key      [32]byte
	deadline time.Time
	handler  RemoteAskPassHandler

	ctx       context.Context
	cancel    context.CancelFunc
	usedMu    sync.Mutex
	used      map[[32]byte]struct{}
	handlerMu sync.Mutex
	closeOnce sync.Once
	wg        sync.WaitGroup
}

type remoteAskPassRequest struct {
	Version int    `json:"version"`
	Token   string `json:"token"`
	Prompt  string `json:"prompt"`
	MAC     string `json:"mac"`
}

type remoteAskPassSealedResponse struct {
	Version    int    `json:"version"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

type remoteAskPassResponsePayload struct {
	Value string `json:"value,omitempty"`
	Code  string `json:"code,omitempty"`
}

func StartRemoteAskPassBroker(parent context.Context, lifetime time.Duration, handler RemoteAskPassHandler) (*RemoteAskPassBroker, error) {
	if parent == nil {
		return nil, errors.New("AskPass parent context is required")
	}
	if lifetime <= 0 {
		return nil, errors.New("AskPass lifetime must be positive")
	}
	if handler == nil {
		return nil, errors.New("AskPass handler is required")
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("start AskPass loopback broker: %w", err)
	}
	deadline := time.Now().Add(lifetime)
	if parentDeadline, ok := parent.Deadline(); ok && parentDeadline.Before(deadline) {
		deadline = parentDeadline
	}
	ctx, cancel := context.WithDeadline(parent, deadline)
	broker := &RemoteAskPassBroker{
		listener: listener,
		endpoint: listener.Addr().String(),
		deadline: deadline,
		handler:  handler,
		ctx:      ctx,
		cancel:   cancel,
		used:     make(map[[32]byte]struct{}),
	}
	if _, err := rand.Read(broker.key[:]); err != nil {
		cancel()
		listener.Close()
		return nil, fmt.Errorf("generate AskPass session key: %w", err)
	}
	broker.wg.Add(1)
	go broker.acceptLoop()
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	return broker, nil
}

func (b *RemoteAskPassBroker) Deadline() time.Time { return b.deadline }

// SSHEnvironment returns only the controlled variables that must override the
// ssh child's environment. The caller merges them without logging their values.
func (b *RemoteAskPassBroker) SSHEnvironment(helperPath string) ([]string, error) {
	if b == nil {
		return nil, errors.New("AskPass broker is required")
	}
	if strings.IndexByte(helperPath, 0) >= 0 || !filepath.IsAbs(helperPath) {
		return nil, errors.New("AskPass helper path must be absolute")
	}
	select {
	case <-b.ctx.Done():
		return nil, errors.New("AskPass broker has expired")
	default:
	}
	return []string{
		"SSH_ASKPASS=" + helperPath,
		"SSH_ASKPASS_REQUIRE=force",
		remoteAskPassModeEnv + "=1",
		remoteAskPassEndpointEnv + "=" + b.endpoint,
		remoteAskPassKeyEnv + "=" + base64.RawURLEncoding.EncodeToString(b.key[:]),
		remoteAskPassDeadlineEnv + "=" + strconv.FormatInt(b.deadline.UnixMilli(), 10),
	}, nil
}

func (b *RemoteAskPassBroker) Close() error {
	if b == nil {
		return nil
	}
	b.closeOnce.Do(func() {
		b.cancel()
		_ = b.listener.Close()
		b.wg.Wait()
		for i := range b.key {
			b.key[i] = 0
		}
		b.usedMu.Lock()
		clear(b.used)
		b.usedMu.Unlock()
	})
	return nil
}

func (b *RemoteAskPassBroker) acceptLoop() {
	defer b.wg.Done()
	for {
		connection, err := b.listener.Accept()
		if err != nil {
			select {
			case <-b.ctx.Done():
				return
			default:
				continue
			}
		}
		b.wg.Add(1)
		go func() {
			defer b.wg.Done()
			defer connection.Close()
			b.serveConnection(connection)
		}()
	}
}

func (b *RemoteAskPassBroker) serveConnection(connection net.Conn) {
	remote, ok := connection.RemoteAddr().(*net.TCPAddr)
	if !ok || !remote.IP.IsLoopback() || time.Now().After(b.deadline) {
		return
	}
	_ = connection.SetDeadline(b.deadline)
	decoder := json.NewDecoder(bufio.NewReader(io.LimitReader(connection, remoteAskPassMaxFrameBytes+1)))
	decoder.DisallowUnknownFields()
	var request remoteAskPassRequest
	if err := decoder.Decode(&request); err != nil || request.Version != remoteAskPassProtocolVersion {
		return
	}
	if len(request.Prompt) == 0 || len(request.Prompt) > remoteAskPassMaxPromptBytes || strings.IndexByte(request.Prompt, 0) >= 0 {
		return
	}
	tokenBytes, err := base64.RawURLEncoding.DecodeString(request.Token)
	if err != nil || len(tokenBytes) != 32 {
		return
	}
	var token [32]byte
	copy(token[:], tokenBytes)
	wantMAC := remoteAskPassRequestMAC(b.key[:], token[:], request.Prompt)
	providedMAC, err := base64.RawURLEncoding.DecodeString(request.MAC)
	if err != nil || subtle.ConstantTimeCompare(providedMAC, wantMAC) != 1 {
		return
	}
	if !b.consumeToken(token) {
		b.writeSealedResponse(connection, token[:], remoteAskPassResponsePayload{Code: "request_replayed"})
		return
	}

	classifiedPrompt := sanitizeRemoteAskPassPromptLimit(request.Prompt, remoteAskPassMaxPromptBytes)
	prompt := RemoteAskPassPrompt{
		Kind:    ClassifyRemoteAskPassPrompt(classifiedPrompt),
		Message: truncateRemoteAskPassPrompt(classifiedPrompt, 4096),
	}
	if prompt.Kind == RemoteAskPassHostKeyChanged {
		b.writeSealedResponse(connection, token[:], remoteAskPassResponsePayload{Code: "host_key_changed"})
		return
	}
	b.handlerMu.Lock()
	answer, handlerErr := b.handler(b.ctx, prompt)
	b.handlerMu.Unlock()
	if handlerErr != nil || !answer.Accepted {
		b.writeSealedResponse(connection, token[:], remoteAskPassResponsePayload{Code: "rejected"})
		return
	}
	value := answer.Value
	if prompt.Kind == RemoteAskPassHostKeyConfirm {
		value = "yes"
	}
	if len(value) > remoteAskPassMaxAnswerBytes || strings.ContainsAny(value, "\x00\r\n") {
		b.writeSealedResponse(connection, token[:], remoteAskPassResponsePayload{Code: "invalid_answer"})
		return
	}
	b.writeSealedResponse(connection, token[:], remoteAskPassResponsePayload{Value: value})
}

func (b *RemoteAskPassBroker) consumeToken(token [32]byte) bool {
	b.usedMu.Lock()
	defer b.usedMu.Unlock()
	if _, exists := b.used[token]; exists {
		return false
	}
	b.used[token] = struct{}{}
	return true
}

func (b *RemoteAskPassBroker) writeSealedResponse(writer io.Writer, token []byte, payload remoteAskPassResponsePayload) {
	block, err := aes.NewCipher(b.key[:])
	if err != nil {
		return
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return
	}
	plain, err := json.Marshal(payload)
	if err != nil {
		return
	}
	sealed := gcm.Seal(nil, nonce, plain, token)
	_ = json.NewEncoder(writer).Encode(remoteAskPassSealedResponse{
		Version:    remoteAskPassProtocolVersion,
		Nonce:      base64.RawURLEncoding.EncodeToString(nonce),
		Ciphertext: base64.RawURLEncoding.EncodeToString(sealed),
	})
}

func remoteAskPassRequestMAC(key, token []byte, prompt string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("reasonix-askpass-v1\x00"))
	_, _ = mac.Write(token)
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(prompt))
	return mac.Sum(nil)
}

func ClassifyRemoteAskPassPrompt(prompt string) RemoteAskPassPromptKind {
	lower := strings.ToLower(prompt)
	switch {
	case strings.Contains(lower, "remote host identification has changed"),
		strings.Contains(lower, "possible dns spoofing detected"),
		strings.Contains(lower, "host key verification failed"),
		strings.Contains(lower, "offending ") && strings.Contains(lower, " key"):
		return RemoteAskPassHostKeyChanged
	case strings.Contains(lower, "authenticity of host") ||
		strings.Contains(lower, "are you sure you want to continue connecting"):
		return RemoteAskPassHostKeyConfirm
	case strings.Contains(lower, "passphrase for key") || strings.Contains(lower, "enter passphrase"):
		return RemoteAskPassKeyPassphrase
	case strings.Contains(lower, "verification code") || strings.Contains(lower, "one-time password") ||
		strings.Contains(lower, "one time password") || strings.Contains(lower, "otp") ||
		strings.Contains(lower, "authentication code"):
		return RemoteAskPassVerification
	case strings.Contains(lower, "password"):
		return RemoteAskPassPassword
	default:
		return RemoteAskPassAuthentication
	}
}

func sanitizeRemoteAskPassPromptLimit(prompt string, maxVisibleBytes int) string {
	var builder strings.Builder
	builder.Grow(min(len(prompt), maxVisibleBytes))
	lastWasSpace := false
	for i := 0; i < len(prompt); {
		if prompt[i] == 0x1b {
			i++
			if i >= len(prompt) {
				break
			}
			switch prompt[i] {
			case '[': // CSI: consume through its final byte.
				i++
				for i < len(prompt) {
					value := prompt[i]
					i++
					if value >= 0x40 && value <= 0x7e {
						break
					}
				}
			case ']': // OSC: consume through BEL or ST (ESC backslash).
				i++
				for i < len(prompt) {
					if prompt[i] == 0x07 {
						i++
						break
					}
					if prompt[i] == 0x1b && i+1 < len(prompt) && prompt[i+1] == '\\' {
						i += 2
						break
					}
					i++
				}
			default:
				i++
			}
			continue
		}
		r, size := utf8.DecodeRuneInString(prompt[i:])
		if r == utf8.RuneError && size == 1 {
			i++
			continue
		}
		i += size
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			if r == '\r' || r == '\n' || r == '\t' {
				if builder.Len() > 0 && !lastWasSpace {
					builder.WriteByte(' ')
					lastWasSpace = true
				}
			}
			continue
		}
		if builder.Len()+size > maxVisibleBytes {
			builder.WriteString("...")
			break
		}
		builder.WriteRune(r)
		lastWasSpace = r == ' '
	}
	return strings.TrimSpace(builder.String())
}

func truncateRemoteAskPassPrompt(prompt string, maxBytes int) string {
	if len(prompt) <= maxBytes {
		return prompt
	}
	limit := maxBytes - 3
	for limit > 0 && !utf8.RuneStart(prompt[limit]) {
		limit--
	}
	return prompt[:limit] + "..."
}

// RunRemoteAskPassHelper is the callable early-mode entry for the Desktop
// executable. main integration may call it before Wails initialization and exit
// with the returned code when handled is true.
func RunRemoteAskPassHelper(ctx context.Context, args []string, getenv func(string) string, stdout io.Writer) (handled bool, exitCode int) {
	if getenv == nil || getenv(remoteAskPassModeEnv) != "1" {
		return false, 0
	}
	if ctx == nil || stdout == nil || len(args) != 1 {
		return true, 1
	}
	key, endpoint, deadline, err := remoteAskPassHelperConfig(getenv)
	if err != nil {
		return true, 1
	}
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return true, 1
	}
	value, err := remoteAskPassExchangeWithToken(ctx, endpoint, key, deadline, args[0], token)
	if err != nil {
		return true, 1
	}
	if _, err := io.WriteString(stdout, value+"\n"); err != nil {
		return true, 1
	}
	return true, 0
}

func remoteAskPassHelperConfig(getenv func(string) string) ([32]byte, string, time.Time, error) {
	var key [32]byte
	keyBytes, err := base64.RawURLEncoding.DecodeString(getenv(remoteAskPassKeyEnv))
	if err != nil || len(keyBytes) != len(key) {
		return key, "", time.Time{}, errors.New("invalid AskPass capability")
	}
	copy(key[:], keyBytes)
	endpoint := getenv(remoteAskPassEndpointEnv)
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil || port == "" {
		return key, "", time.Time{}, errors.New("invalid AskPass endpoint")
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() || ip.To4() == nil {
		return key, "", time.Time{}, errors.New("AskPass endpoint is not IPv4 loopback")
	}
	deadlineMillis, err := strconv.ParseInt(getenv(remoteAskPassDeadlineEnv), 10, 64)
	if err != nil {
		return key, "", time.Time{}, errors.New("invalid AskPass deadline")
	}
	deadline := time.UnixMilli(deadlineMillis)
	if !time.Now().Before(deadline) {
		return key, "", time.Time{}, errors.New("AskPass capability expired")
	}
	return key, endpoint, deadline, nil
}

func remoteAskPassExchangeWithToken(ctx context.Context, endpoint string, key [32]byte, deadline time.Time, prompt string, token [32]byte) (string, error) {
	if len(prompt) == 0 || len(prompt) > remoteAskPassMaxPromptBytes || strings.IndexByte(prompt, 0) >= 0 {
		return "", errors.New("invalid AskPass prompt")
	}
	deadlineCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	ctx = deadlineCtx
	dialer := net.Dialer{}
	connection, err := dialer.DialContext(ctx, "tcp4", endpoint)
	if err != nil {
		return "", errors.New("AskPass broker unavailable")
	}
	defer connection.Close()
	_ = connection.SetDeadline(deadline)
	request := remoteAskPassRequest{
		Version: remoteAskPassProtocolVersion,
		Token:   base64.RawURLEncoding.EncodeToString(token[:]),
		Prompt:  prompt,
		MAC:     base64.RawURLEncoding.EncodeToString(remoteAskPassRequestMAC(key[:], token[:], prompt)),
	}
	if err := json.NewEncoder(connection).Encode(request); err != nil {
		return "", errors.New("send AskPass request")
	}
	decoder := json.NewDecoder(bufio.NewReader(io.LimitReader(connection, remoteAskPassMaxFrameBytes+1)))
	decoder.DisallowUnknownFields()
	var response remoteAskPassSealedResponse
	if err := decoder.Decode(&response); err != nil || response.Version != remoteAskPassProtocolVersion {
		return "", errors.New("invalid AskPass response")
	}
	nonce, err := base64.RawURLEncoding.DecodeString(response.Nonce)
	if err != nil {
		return "", errors.New("invalid AskPass response")
	}
	sealed, err := base64.RawURLEncoding.DecodeString(response.Ciphertext)
	if err != nil {
		return "", errors.New("invalid AskPass response")
	}
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", errors.New("invalid AskPass response")
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(nonce) != gcm.NonceSize() {
		return "", errors.New("invalid AskPass response")
	}
	plain, err := gcm.Open(nil, nonce, sealed, token[:])
	if err != nil {
		return "", errors.New("unauthenticated AskPass response")
	}
	var payload remoteAskPassResponsePayload
	if err := json.Unmarshal(plain, &payload); err != nil {
		return "", errors.New("invalid AskPass response")
	}
	if payload.Code != "" {
		return "", fmt.Errorf("AskPass request failed: %s", payload.Code)
	}
	if len(payload.Value) > remoteAskPassMaxAnswerBytes || strings.ContainsAny(payload.Value, "\x00\r\n") {
		return "", errors.New("invalid AskPass answer")
	}
	return payload.Value, nil
}
