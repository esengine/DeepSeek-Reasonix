package sessionstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/provider"
)

// NativeArchive includes resolved resources; importing it grants no execution authority.
type NativeArchive struct {
	Format  string             `json:"format"`
	Version int                `json:"version"`
	Records []provider.Message `json:"records"`
}

const NativeArchiveFormat = "reasonix-native-session"
const NativeArchiveVersion = 2
const NativeArchiveLiveVersion = 3
const MaxNativeArchiveBytes = 192 << 20

var ErrNativeArchiveInvalid = errors.New("invalid native session archive")
var ErrNativeArchiveUnsupported = errors.New("unsupported native session archive version")

func ExportNativeArchive(path string) ([]byte, error) {
	if err := refuseBoundConversationTransfer(path); err != nil {
		return nil, err
	}
	s, err := LoadSession(path)
	if err != nil {
		return nil, err
	}
	archive := NativeArchive{Format: NativeArchiveFormat, Version: NativeArchiveVersion, Records: s.Snapshot()}
	if summary := s.CompositionSummary(); summary != nil && summary.FormatVersion != 0 {
		if summary.FormatVersion != agentcomposition.LiveBindingVersion {
			return nil, ErrNativeArchiveUnsupported
		}
		if err := s.Composition().Validate(); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrNativeArchiveInvalid, err)
		}
		archive.Version = NativeArchiveLiveVersion
	}
	data, err := json.Marshal(archive)
	if len(data) > MaxNativeArchiveBytes {
		return nil, ErrSessionReplayLimitExceeded
	}
	return data, err
}

func DecodeNativeArchive(data []byte) (*Session, error) {
	if len(data) > MaxNativeArchiveBytes {
		return nil, ErrSessionReplayLimitExceeded
	}
	var archive NativeArchive
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&archive); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNativeArchiveInvalid, err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("%w: trailing data", ErrNativeArchiveInvalid)
	}
	if archive.Format != NativeArchiveFormat || (archive.Version != NativeArchiveVersion && archive.Version != NativeArchiveLiveVersion) {
		return nil, fmt.Errorf("%w: %d", ErrNativeArchiveUnsupported, archive.Version)
	}
	if len(archive.Records) > sessionEventReplayMaxMessages {
		return nil, ErrSessionReplayLimitExceeded
	}
	var binding *agentcomposition.Binding
	for i, m := range archive.Records {
		if m.AgentComposition == nil {
			continue
		}
		if i != 0 || m.Role != provider.RoleSystem || binding != nil {
			return nil, agentcomposition.ErrInvalid
		}
		binding = m.AgentComposition
		if binding.Live() && archive.Version != NativeArchiveLiveVersion {
			return nil, ErrNativeArchiveUnsupported
		}
		if err := binding.Validate(); err != nil {
			return nil, err
		}
	}
	s := NewSession("")
	s.Messages = archive.Records
	if !s.HasSystemMessage() {
		return nil, fmt.Errorf("%w: no system message", ErrNativeArchiveInvalid)
	}
	if binding != nil && s.HasContent() && !binding.Started {
		return nil, fmt.Errorf("%w: admitted archive is unlocked", agentcomposition.ErrInvalid)
	}
	return s, nil
}
