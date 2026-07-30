package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"reasonix/internal/fileutil"
)

const (
	remoteHostStoreVersion  = 2
	remoteHostStoreVersion1 = 1
	remoteHostStoreMaxBytes = 1 << 20
	remoteHostStoreMaxHosts = 256
	defaultRemoteSSHPort    = 22
)

type RemoteHostConnectionMode string

const (
	RemoteHostConnectionDirect RemoteHostConnectionMode = "direct"
	RemoteHostConnectionConfig RemoteHostConnectionMode = "config"
)

var (
	// ErrRemoteHostStoreCorrupt means the on-disk store was not accepted.  A
	// caller must not treat this as an empty store and overwrite it: doing so
	// could silently discard the only persisted lease needed for a reconnect.
	ErrRemoteHostStoreCorrupt = errors.New("remote host store is corrupt")
	// ErrRemoteHostStoreUnsafe means the path is not a private regular file.
	ErrRemoteHostStoreUnsafe = errors.New("remote host store is unsafe")

	remoteHostAliasPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,254}$`)
	remoteSSHUsernamePattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,63}$`)
	remoteDNSLabelPattern    = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)
	remoteNumericHostPattern = regexp.MustCompile(`^[0-9.]+$`)
	remoteHostIDPattern      = regexp.MustCompile(`^host_[0-9a-f]{64}$`)
	remoteClientIDPattern    = regexp.MustCompile(`^desktop_[0-9a-f]{64}$`)
	remoteHostPathLocks      sync.Map // canonical path -> *sync.Mutex
)

// RemoteHostEntry is deliberately a secret-free record.  SSH credentials,
// private-key passphrases, passwords and AskPass material never belong here.
// Direct entries carry only a validated username, Host and port; advanced
// entries retain an OpenSSH config alias.
type RemoteHostEntry struct {
	ID               string                   `json:"id"`
	Mode             RemoteHostConnectionMode `json:"mode"`
	Destination      string                   `json:"destination,omitempty"`
	Port             int                      `json:"port,omitempty"`
	Alias            string                   `json:"alias,omitempty"`
	Label            string                   `json:"label"`
	SSHConfigPath    string                   `json:"sshConfigPath,omitempty"`
	ClientInstanceID string                   `json:"clientInstanceId"`
	ResumeLeaseID    string                   `json:"resumeLeaseId,omitempty"`
	LayoutRef        string                   `json:"layoutRef,omitempty"`
}

type remoteHostStoreDocument struct {
	Version int               `json:"version"`
	Hosts   []RemoteHostEntry `json:"hosts"`
}

// RemoteHostStore serializes all in-process access to one canonical path.  The
// write itself is a sibling-temp + fsync + atomic replace, so readers observe
// either the old complete document or the new complete document.
type RemoteHostStore struct {
	path string
	mu   *sync.Mutex
}

func NewRemoteHostStore(path string) (*RemoteHostStore, error) {
	if strings.TrimSpace(path) == "" || strings.IndexByte(path, 0) >= 0 {
		return nil, fmt.Errorf("remote host store path is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve remote host store path: %w", err)
	}
	absolute = filepath.Clean(absolute)
	lock, _ := remoteHostPathLocks.LoadOrStore(absolute, &sync.Mutex{})
	return &RemoteHostStore{path: absolute, mu: lock.(*sync.Mutex)}, nil
}

func (s *RemoteHostStore) Path() string { return s.path }

// NewRemoteHostEntry creates the stable 256-bit client identity owned by one
// saved Host entry.  Reconnects reuse it; different Host entries never do.
func NewRemoteHostEntry(alias, label string) (RemoteHostEntry, error) {
	return newRemoteHostEntry(RemoteHostEntry{Mode: RemoteHostConnectionConfig, Alias: alias, Label: label})
}

func NewRemoteDirectHostEntry(destination string, port int, label string) (RemoteHostEntry, error) {
	target, err := ParseRemoteSSHDirectDestination(destination)
	if err != nil {
		return RemoteHostEntry{}, err
	}
	if err := ValidateRemoteSSHPort(port); err != nil {
		return RemoteHostEntry{}, err
	}
	return newRemoteHostEntry(RemoteHostEntry{
		Mode: RemoteHostConnectionDirect, Destination: target.Destination(), Port: port, Label: label,
	})
}

func newRemoteHostEntry(entry RemoteHostEntry) (RemoteHostEntry, error) {
	var entryEntropy, clientEntropy [32]byte
	if _, err := rand.Read(entryEntropy[:]); err != nil {
		return RemoteHostEntry{}, fmt.Errorf("generate remote Host identity: %w", err)
	}
	if _, err := rand.Read(clientEntropy[:]); err != nil {
		return RemoteHostEntry{}, fmt.Errorf("generate remote client identity: %w", err)
	}
	entry.ID = "host_" + hex.EncodeToString(entryEntropy[:])
	entry.ClientInstanceID = "desktop_" + hex.EncodeToString(clientEntropy[:])
	if err := validateRemoteHostEntry(entry); err != nil {
		return RemoteHostEntry{}, err
	}
	return entry, nil
}

type RemoteSSHDirectTarget struct {
	Username string
	Host     string
}

func (t RemoteSSHDirectTarget) Destination() string {
	host := t.Host
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return t.Username + "@" + host
}

// ParseRemoteSSHDirectDestination validates the user-facing username@host
// form and returns canonical argv values. IPv6 literals must be bracketed so
// the separator remains unambiguous.
func ParseRemoteSSHDirectDestination(destination string) (RemoteSSHDirectTarget, error) {
	if !utf8.ValidString(destination) || len(destination) > 384 || strings.TrimSpace(destination) != destination || strings.Count(destination, "@") != 1 {
		return RemoteSSHDirectTarget{}, errors.New("remote SSH destination must be username@host")
	}
	username, host, _ := strings.Cut(destination, "@")
	if !remoteSSHUsernamePattern.MatchString(username) || strings.HasPrefix(username, "-") {
		return RemoteSSHDirectTarget{}, errors.New("remote SSH username is invalid")
	}
	canonicalHost, err := canonicalRemoteSSHHost(host)
	if err != nil {
		return RemoteSSHDirectTarget{}, err
	}
	return RemoteSSHDirectTarget{Username: username, Host: canonicalHost}, nil
}

func canonicalRemoteSSHHost(host string) (string, error) {
	if host == "" || strings.HasPrefix(host, "-") {
		return "", errors.New("remote SSH host is invalid")
	}
	if strings.HasPrefix(host, "[") || strings.HasSuffix(host, "]") {
		if len(host) < 3 || host[0] != '[' || host[len(host)-1] != ']' {
			return "", errors.New("remote SSH IPv6 host must use [address] form")
		}
		ip := net.ParseIP(host[1 : len(host)-1])
		if ip == nil || ip.To4() != nil {
			return "", errors.New("remote SSH bracketed host must be an IPv6 address")
		}
		return ip.String(), nil
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.To4() == nil {
			return "", errors.New("remote SSH IPv6 host must use [address] form")
		}
		return ip.String(), nil
	}
	if strings.Contains(host, ":") {
		return "", errors.New("remote SSH IPv6 host must use [address] form")
	}
	if len(host) > 253 || remoteNumericHostPattern.MatchString(host) {
		return "", errors.New("remote SSH host is invalid")
	}
	trimmed := strings.TrimSuffix(host, ".")
	if trimmed == "" || len(trimmed) > 253 {
		return "", errors.New("remote SSH host is invalid")
	}
	for _, label := range strings.Split(trimmed, ".") {
		if !remoteDNSLabelPattern.MatchString(label) {
			return "", errors.New("remote SSH host is invalid")
		}
	}
	return strings.ToLower(trimmed), nil
}

func ValidateRemoteSSHPort(port int) error {
	if port < 1 || port > 65535 {
		return errors.New("remote SSH port must be between 1 and 65535")
	}
	return nil
}

func ValidateRemoteHostAlias(alias string) error {
	if !remoteHostAliasPattern.MatchString(alias) {
		return fmt.Errorf("invalid SSH Host alias %q", alias)
	}
	return nil
}

func ValidateRemoteHostEntryID(entryID string) error {
	if !remoteHostIDPattern.MatchString(entryID) {
		return errors.New("invalid remote Host entry id")
	}
	return nil
}

func (s *RemoteHostStore) Load() ([]RemoteHostEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

func (s *RemoteHostStore) Upsert(entry RemoteHostEntry) error {
	if err := validateRemoteHostEntry(entry); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	hosts, err := s.loadLocked()
	if err != nil {
		return err
	}
	replaced := false
	for i := range hosts {
		if hosts[i].ID == entry.ID {
			hosts[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		hosts = append(hosts, entry)
	}
	return s.saveLocked(hosts)
}

func (s *RemoteHostStore) Delete(entryID string) error {
	if err := ValidateRemoteHostEntryID(entryID); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	hosts, err := s.loadLocked()
	if err != nil {
		return err
	}
	filtered := hosts[:0]
	for _, host := range hosts {
		if host.ID != entryID {
			filtered = append(filtered, host)
		}
	}
	if len(filtered) == len(hosts) {
		return nil
	}
	return s.saveLocked(filtered)
}

func (s *RemoteHostStore) UpdateResumeLease(entryID, leaseID string) error {
	return s.update(entryID, func(host *RemoteHostEntry) { host.ResumeLeaseID = leaseID })
}

func (s *RemoteHostStore) UpdateLayoutRef(entryID, layoutRef string) error {
	return s.update(entryID, func(host *RemoteHostEntry) { host.LayoutRef = layoutRef })
}

func (s *RemoteHostStore) update(entryID string, mutate func(*RemoteHostEntry)) error {
	if err := ValidateRemoteHostEntryID(entryID); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	hosts, err := s.loadLocked()
	if err != nil {
		return err
	}
	for i := range hosts {
		if hosts[i].ID != entryID {
			continue
		}
		mutate(&hosts[i])
		if err := validateRemoteHostEntry(hosts[i]); err != nil {
			return err
		}
		return s.saveLocked(hosts)
	}
	return fmt.Errorf("remote Host entry %q is not saved", entryID)
}

func (s *RemoteHostStore) Get(entryID string) (RemoteHostEntry, bool, error) {
	if err := ValidateRemoteHostEntryID(entryID); err != nil {
		return RemoteHostEntry{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	hosts, err := s.loadLocked()
	if err != nil {
		return RemoteHostEntry{}, false, err
	}
	for _, host := range hosts {
		if host.ID == entryID {
			return host, true, nil
		}
	}
	return RemoteHostEntry{}, false, nil
}

func (s *RemoteHostStore) loadLocked() ([]RemoteHostEntry, error) {
	file, info, err := openRemoteHostStoreFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return []RemoteHostEntry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open remote host store: %w", err)
	}
	defer file.Close()
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: path is not a regular file", ErrRemoteHostStoreUnsafe)
	}
	if err := validateRemoteHostStorePermissions(info); err != nil {
		return nil, err
	}
	if info.Size() < 0 || info.Size() > remoteHostStoreMaxBytes {
		return nil, fmt.Errorf("%w: document exceeds %d bytes", ErrRemoteHostStoreCorrupt, remoteHostStoreMaxBytes)
	}
	raw, err := io.ReadAll(io.LimitReader(file, remoteHostStoreMaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read remote host store: %w", err)
	}
	if len(raw) > remoteHostStoreMaxBytes {
		return nil, fmt.Errorf("%w: document exceeds %d bytes", ErrRemoteHostStoreCorrupt, remoteHostStoreMaxBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var document remoteHostStoreDocument
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRemoteHostStoreCorrupt, err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRemoteHostStoreCorrupt, err)
	}
	if document.Version != remoteHostStoreVersion && document.Version != remoteHostStoreVersion1 {
		return nil, fmt.Errorf("%w: unsupported version %d", ErrRemoteHostStoreCorrupt, document.Version)
	}
	if document.Version == remoteHostStoreVersion1 {
		for index := range document.Hosts {
			document.Hosts[index].Mode = RemoteHostConnectionConfig
		}
	}
	if len(document.Hosts) > remoteHostStoreMaxHosts {
		return nil, fmt.Errorf("%w: too many Host entries", ErrRemoteHostStoreCorrupt)
	}
	seenIDs := make(map[string]struct{}, len(document.Hosts))
	seenConnections := make(map[string]struct{}, len(document.Hosts))
	for _, host := range document.Hosts {
		if err := validateRemoteHostEntry(host); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrRemoteHostStoreCorrupt, err)
		}
		if _, exists := seenIDs[host.ID]; exists {
			return nil, fmt.Errorf("%w: duplicate Host entry id %q", ErrRemoteHostStoreCorrupt, host.ID)
		}
		seenIDs[host.ID] = struct{}{}
		connectionKey := remoteHostConnectionKey(host)
		if _, exists := seenConnections[connectionKey]; exists {
			return nil, fmt.Errorf("%w: duplicate SSH Host entry", ErrRemoteHostStoreCorrupt)
		}
		seenConnections[connectionKey] = struct{}{}
	}
	return append([]RemoteHostEntry(nil), document.Hosts...), nil
}

func (s *RemoteHostStore) saveLocked(hosts []RemoteHostEntry) error {
	if len(hosts) > remoteHostStoreMaxHosts {
		return fmt.Errorf("too many remote Host entries")
	}
	copyOfHosts := append([]RemoteHostEntry(nil), hosts...)
	seenIDs := make(map[string]struct{}, len(copyOfHosts))
	seenConnections := make(map[string]struct{}, len(copyOfHosts))
	for _, host := range copyOfHosts {
		if err := validateRemoteHostEntry(host); err != nil {
			return err
		}
		if _, exists := seenIDs[host.ID]; exists {
			return fmt.Errorf("duplicate remote Host entry id %q", host.ID)
		}
		seenIDs[host.ID] = struct{}{}
		connectionKey := remoteHostConnectionKey(host)
		if _, exists := seenConnections[connectionKey]; exists {
			return fmt.Errorf("duplicate SSH Host entry %q", remoteHostDisplayConnection(host))
		}
		seenConnections[connectionKey] = struct{}{}
	}
	sort.Slice(copyOfHosts, func(i, j int) bool { return copyOfHosts[i].ID < copyOfHosts[j].ID })
	raw, err := json.MarshalIndent(remoteHostStoreDocument{Version: remoteHostStoreVersion, Hosts: copyOfHosts}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode remote host store: %w", err)
	}
	raw = append(raw, '\n')
	if len(raw) > remoteHostStoreMaxBytes {
		return fmt.Errorf("remote host store exceeds %d bytes", remoteHostStoreMaxBytes)
	}
	if err := fileutil.AtomicWriteFile(s.path, raw, 0o600); err != nil {
		return fmt.Errorf("write remote host store: %w", err)
	}
	return nil
}

func validateRemoteHostEntry(host RemoteHostEntry) error {
	if err := ValidateRemoteHostEntryID(host.ID); err != nil {
		return err
	}
	switch host.Mode {
	case RemoteHostConnectionDirect:
		target, err := ParseRemoteSSHDirectDestination(host.Destination)
		if err != nil {
			return err
		}
		if target.Destination() != host.Destination {
			return errors.New("remote SSH destination must be canonical")
		}
		if err := ValidateRemoteSSHPort(host.Port); err != nil {
			return err
		}
		if host.Alias != "" || host.SSHConfigPath != "" {
			return errors.New("direct remote Host must not contain OpenSSH config fields")
		}
	case RemoteHostConnectionConfig:
		if err := ValidateRemoteHostAlias(host.Alias); err != nil {
			return err
		}
		if err := validateRemoteSSHConfigPath(host.SSHConfigPath); err != nil {
			return err
		}
		if host.Destination != "" || host.Port != 0 {
			return errors.New("config remote Host must not contain direct connection fields")
		}
	default:
		return errors.New("remote Host connection mode is invalid")
	}
	if err := validateRemoteHostText("label", host.Label, 256, false); err != nil {
		return err
	}
	if !remoteClientIDPattern.MatchString(host.ClientInstanceID) {
		return errors.New("remote Host clientInstanceId must be a generated 256-bit identity")
	}
	if err := validateRemoteHostText("resumeLeaseId", host.ResumeLeaseID, 512, true); err != nil {
		return err
	}
	if err := validateRemoteHostText("layoutRef", host.LayoutRef, 512, true); err != nil {
		return err
	}
	return nil
}

func validateRemoteSSHConfigPath(path string) error {
	if path == "" {
		return nil
	}
	if !utf8.ValidString(path) || strings.IndexByte(path, 0) >= 0 {
		return errors.New("remote Host sshConfigPath is invalid")
	}
	for _, r := range path {
		if r < 0x20 || r == 0x7f {
			return errors.New("remote Host sshConfigPath contains a control character")
		}
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("remote Host sshConfigPath must be an absolute clean path")
	}
	return nil
}

func remoteHostConnectionKey(host RemoteHostEntry) string {
	switch host.Mode {
	case RemoteHostConnectionDirect:
		return string(host.Mode) + "\x00" + host.Destination + "\x00" + strconv.Itoa(host.Port)
	case RemoteHostConnectionConfig:
		return string(host.Mode) + "\x00" + host.SSHConfigPath + "\x00" + host.Alias
	default:
		return string(host.Mode)
	}
}

func remoteHostDisplayConnection(host RemoteHostEntry) string {
	if host.Mode == RemoteHostConnectionDirect {
		target, err := ParseRemoteSSHDirectDestination(host.Destination)
		if err == nil {
			return target.Username + "@" + net.JoinHostPort(target.Host, strconv.Itoa(host.Port))
		}
		return host.Destination
	}
	return host.Alias
}

func validateRemoteHostText(field, value string, maxBytes int, allowEmpty bool) error {
	if value == "" {
		if allowEmpty {
			return nil
		}
		return fmt.Errorf("remote Host %s is required", field)
	}
	if len(value) > maxBytes || !utf8.ValidString(value) {
		return fmt.Errorf("remote Host %s is invalid", field)
	}
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("remote Host %s must not have surrounding whitespace", field)
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("remote Host %s contains a control character", field)
		}
	}
	return nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}
