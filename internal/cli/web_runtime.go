package cli

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"reasonix/internal/config"
	"reasonix/internal/fileutil"
	"reasonix/internal/store"
)

func serveConfigWithCommandDefaults(command string, authExplicit bool, cfg config.ServeConfig) config.ServeConfig {
	if command == "web" && !authExplicit {
		cfg.AuthMode = "token"
	}
	return cfg
}

const (
	webPortRetryLimit        = 100
	webInstanceHeartbeat     = 15 * time.Second
	maxWebSessionIDBytes     = 218 // leaves room for .jsonl and session sidecars
	webInstanceDirectoryName = "instances"
)

// listenWebWithPortRetry binds addr, walking port+1 only when a concrete port
// is already occupied. Port 0 remains an ordinary kernel-assigned ephemeral
// bind, and non-EADDRINUSE failures are returned immediately.
func listenWebWithPortRetry(addr string) (net.Listener, error) {
	host, rawPort, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil || port < 0 || port > 65535 {
		return nil, fmt.Errorf("invalid listen port %q", rawPort)
	}
	if port == 0 {
		return net.Listen("tcp", addr)
	}
	for attempt := 0; ; attempt++ {
		candidate := net.JoinHostPort(host, strconv.Itoa(port))
		ln, listenErr := net.Listen("tcp", candidate)
		if listenErr == nil {
			return ln, nil
		}
		if !webAddressInUse(listenErr) || attempt >= webPortRetryLimit || port >= 65535 {
			return nil, listenErr
		}
		port++
	}
}

func requestedPort(addr string) int {
	_, rawPort, err := net.SplitHostPort(addr)
	if err != nil {
		return -1
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil {
		return -1
	}
	return port
}

func validateWebSessionID(id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("--session-id cannot be empty")
	}
	if !utf8.ValidString(id) || len(id) > maxWebSessionIDBytes {
		return fmt.Errorf("invalid Web session identity %q", id)
	}
	if id == "." || id == ".." || strings.ContainsAny(id, `/\`) || strings.IndexByte(id, 0) >= 0 {
		return fmt.Errorf("invalid Web session identity %q", id)
	}
	if !store.IsSessionTranscriptName(id + ".jsonl") {
		return fmt.Errorf("invalid Web session identity %q", id)
	}
	return nil
}

func freshWebSessionPath(dir, id string) (string, error) {
	if err := validateWebSessionID(id); err != nil {
		return "", err
	}
	path := filepath.Join(dir, id+".jsonl")
	if _, err := os.Lstat(path); err == nil {
		return "", fmt.Errorf("fresh Web session already exists: %s", path)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	return path, nil
}

// webInstanceRecord is stable for independent operator-facing readers.
// Unknown fields remain forward-compatible with independent readers.
type webInstanceRecord struct {
	ServerID    string `json:"server_id"`
	PID         int    `json:"pid"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	StartedAt   int64  `json:"started_at"`
	HeartbeatAt int64  `json:"heartbeat_at"`
}

type webInstanceRegistry struct {
	dir               string
	now               func() time.Time
	heartbeatInterval time.Duration
	processAlive      func(int) bool
}

type webInstanceRegistration struct {
	path       string
	record     webInstanceRecord
	registry   *webInstanceRegistry
	stop       chan struct{}
	done       chan struct{}
	releaseOne sync.Once
}

func registerWebInstance(reasonixHome, addr string) (*webInstanceRegistration, error) {
	if strings.TrimSpace(reasonixHome) == "" {
		return nil, errors.New("cannot register Web instance: Reasonix home is empty")
	}
	registry := &webInstanceRegistry{
		dir:               filepath.Join(reasonixHome, "server", webInstanceDirectoryName),
		now:               time.Now,
		heartbeatInterval: webInstanceHeartbeat,
		processAlive:      webInstanceProcessAlive,
	}
	return registry.register(addr, os.Getpid())
}

func (r *webInstanceRegistry) register(addr string, pid int) (*webInstanceRegistration, error) {
	if strings.TrimSpace(r.dir) == "" {
		return nil, errors.New("cannot register Web instance: Reasonix home is empty")
	}
	host, rawPort, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("register Web instance: %w", err)
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil || port <= 0 || port > 65535 {
		return nil, fmt.Errorf("register Web instance: invalid bound port %q", rawPort)
	}
	if err := os.MkdirAll(r.dir, 0o700); err != nil {
		return nil, fmt.Errorf("create Web instance registry: %w", err)
	}
	if err := r.sweepStale(); err != nil {
		return nil, fmt.Errorf("sweep Web instance registry: %w", err)
	}

	now := r.now().UnixMilli()
	for range 8 {
		serverID, err := randomWebInstanceID()
		if err != nil {
			return nil, fmt.Errorf("generate Web instance id: %w", err)
		}
		record := webInstanceRecord{
			ServerID:    serverID,
			PID:         pid,
			Host:        host,
			Port:        port,
			StartedAt:   now,
			HeartbeatAt: now,
		}
		path := filepath.Join(r.dir, serverID+".json")
		data, err := json.Marshal(record)
		if err != nil {
			return nil, err
		}
		if err := fileutil.AtomicCreateFile(path, data, 0o600); err != nil {
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return nil, fmt.Errorf("register Web instance: %w", err)
		}
		reg := &webInstanceRegistration{
			path:     path,
			record:   record,
			registry: r,
			stop:     make(chan struct{}),
			done:     make(chan struct{}),
		}
		go reg.heartbeat()
		return reg, nil
	}
	return nil, errors.New("register Web instance: could not allocate a unique id")
}

func randomWebInstanceID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func (r *webInstanceRegistry) sweepStale() error {
	entries, err := os.ReadDir(r.dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(r.dir, entry.Name())
		record, ok := readWebInstanceRecord(path)
		// Malformed files may belong to a newer/live writer. Only delete an entry
		// that can be positively identified as owned by a dead process.
		if !ok || r.processAlive(record.PID) {
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (r *webInstanceRegistry) listLive() ([]webInstanceRecord, error) {
	if err := r.sweepStale(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(r.dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	live := make([]webInstanceRecord, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		if record, ok := readWebInstanceRecord(filepath.Join(r.dir, entry.Name())); ok && r.processAlive(record.PID) {
			live = append(live, record)
		}
	}
	sort.Slice(live, func(i, j int) bool { return live[i].StartedAt < live[j].StartedAt })
	return live, nil
}

func readWebInstanceRecord(path string) (webInstanceRecord, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return webInstanceRecord{}, false
	}
	var record webInstanceRecord
	if json.Unmarshal(data, &record) != nil || record.ServerID == "" || record.PID <= 0 || record.Host == "" || record.Port <= 0 || record.StartedAt <= 0 || record.HeartbeatAt <= 0 {
		return webInstanceRecord{}, false
	}
	return record, true
}

func (r *webInstanceRegistration) heartbeat() {
	defer close(r.done)
	interval := r.registry.heartbeatInterval
	if interval <= 0 {
		<-r.stop
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			r.record.HeartbeatAt = r.registry.now().UnixMilli()
			if data, err := json.Marshal(r.record); err == nil {
				_ = fileutil.AtomicWriteFile(r.path, data, 0o600)
			}
		case <-r.stop:
			return
		}
	}
}

// Release stops heartbeats before removing the single-writer instance file, so
// an in-flight atomic rename cannot recreate a supposedly released entry.
func (r *webInstanceRegistration) Release() {
	if r == nil {
		return
	}
	r.releaseOne.Do(func() {
		close(r.stop)
		<-r.done
		_ = os.Remove(r.path)
	})
}
