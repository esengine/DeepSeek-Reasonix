package sessionstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/state/store"
)

const sessionEnvironmentSchemaVersion = 6
const sessionEnvironmentEventType = "execution_environment"
const environmentEnvelopeLimit = 64 << 10

func isAbsoluteEnvironment(b ExecutionEnvironmentBinding) bool {
	return filepath.IsAbs(b.SourceWorkspace) && filepath.IsAbs(b.WorkspaceRoot) && filepath.IsAbs(b.WorkingCopyRoot)
}

type environmentEnvelope struct {
	SchemaVersion int             `json:"schema_version"`
	Type          string          `json:"type"`
	Binding       json.RawMessage `json:"binding"`
}

func validEnvironmentBinding(path string, b ExecutionEnvironmentBinding) bool {
	return b.Version == 1 && b.SessionPath == canonicalSessionSavePath(path) && b.EnvironmentID != "" && b.Generation != "" &&
		isAbsoluteEnvironment(b)
}

func readEnvironmentFence(path string) (json.RawMessage, ExecutionEnvironmentBinding, bool, error) {
	var b ExecutionEnvironmentBinding
	if len(filepath.Base(SessionEventLogPath(path))) > nameMaxBytes {
		return nil, b, false, nil
	}
	f, err := os.Open(SessionEventLogPath(path))
	if errors.Is(err, os.ErrNotExist) {
		return nil, b, false, nil
	}
	if err != nil {
		return nil, b, false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, b, false, err
	}
	if info.IsDir() {
		return nil, b, false, nil
	}
	raw, err := io.ReadAll(io.LimitReader(f, environmentEnvelopeLimit+1))
	if err != nil {
		return nil, b, false, err
	}
	schema, kind, _ := probeSessionEventHeader(bytes.NewReader(raw))
	if schema != sessionEnvironmentSchemaVersion && kind != sessionEnvironmentEventType {
		return nil, b, false, nil
	}
	var envelope environmentEnvelope
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if decoder.Decode(&envelope) != nil || decoder.InputOffset() > environmentEnvelopeLimit ||
		envelope.SchemaVersion != sessionEnvironmentSchemaVersion || envelope.Type != sessionEnvironmentEventType ||
		json.Unmarshal(envelope.Binding, &b) != nil || !validEnvironmentBinding(path, b) {
		return nil, b, true, ErrExecutionEnvironmentProtocol
	}
	return bytes.Clone(raw[:decoder.InputOffset()]), b, true, nil
}

func loadExecutionEnvironment(path string) (ExecutionEnvironmentBinding, bool, error) {
	if path == "" {
		return ExecutionEnvironmentBinding{}, false, nil
	}
	_, fenced, exists, err := readEnvironmentFence(path)
	if err != nil {
		return fenced, exists, err
	}
	sidecar, sideExists, err := loadExecutionEnvironmentSidecar(path)
	if err != nil {
		return sidecar, true, err
	}
	if exists {
		if sideExists && !reflect.DeepEqual(sidecar, fenced) {
			return fenced, true, ErrExecutionEnvironmentMismatch
		}
		return fenced, true, nil
	}
	return sidecar, sideExists, nil
}

func publishEnvironmentFence(path string, bindingRaw []byte, binding ExecutionEnvironmentBinding) error {
	_, current, exists, err := readEnvironmentFence(path)
	if err != nil {
		return err
	}
	if exists {
		if !reflect.DeepEqual(current, binding) {
			return ErrExecutionEnvironmentMismatch
		}
		return nil
	}
	probe, err := probeSessionEventLog(path)
	if err != nil {
		return err
	}
	if !probe.native || probe.dag || probe.futureSchema || !sessionEventSidecarsFit(path) {
		return ErrExecutionEnvironmentProtocol
	}
	old, err := boundedEnvironmentWAL(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if int64(len(old)) > sessionEventReplayMaxBytes {
		return ErrSessionReplayLimitExceeded
	}
	if len(old) == 0 {
		old, err = checkpointEnvironmentSeed(path)
		if err != nil {
			return err
		}
	}
	header, err := json.Marshal(environmentEnvelope{SchemaVersion: sessionEnvironmentSchemaVersion, Type: sessionEnvironmentEventType, Binding: bindingRaw})
	if err != nil {
		return err
	}
	buf := append(append(header, '\n'), old...)
	return fileutil.AtomicWriteFileStrict(SessionEventLogPath(path), buf, 0600)
}

func checkpointEnvironmentSeed(path string) ([]byte, error) {
	msgs, err := loadSessionMessagesFromJSONLWithLimits(path, defaultSessionReplayLimits)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	digest, err := DigestSessionMessages(msgs)
	if err != nil {
		return nil, err
	}
	meta, _, err := LoadBranchMeta(path)
	if err != nil {
		return nil, err
	}
	rec := sessionEventRecord{SchemaVersion: compositionEventSchema(path, msgs, nil), Type: sessionEventTypeReplace,
		Messages: msgs, ContentDigest: digestString(digest), Revision: meta.Revision}
	raw, err := json.Marshal(rec)
	return append(raw, '\n'), err
}

func prependEnvironmentFence(path string, content []byte) ([]byte, error) {
	raw, _, exists, err := readEnvironmentFence(path)
	if err != nil || !exists {
		return content, err
	}
	return append(append(raw, '\n'), content...), nil
}

func (s *Session) ownsEnvironmentReservation(path string) bool {
	if !s.hasValidWriteAuthority(path) || !s.WriteAuthority().environmentReservation.Load() {
		return false
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		return false
	}
	_, _, exists, err := readEnvironmentFence(path)
	if err != nil || !exists {
		return false
	}
	replay, err := replaySessionEventLog(SessionEventLogPath(path))
	return err == nil && replay.records == 0 && !replay.damaged
}

func boundedEnvironmentWAL(path string) ([]byte, error) {
	f, err := os.Open(SessionEventLogPath(path))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > sessionEventReplayMaxBytes {
		return nil, ErrSessionReplayLimitExceeded
	}
	raw, err := io.ReadAll(io.LimitReader(f, sessionEventReplayMaxBytes+1))
	if int64(len(raw)) > sessionEventReplayMaxBytes {
		return nil, ErrSessionReplayLimitExceeded
	}
	return raw, err
}

func (s *Session) admitEnvironmentSave(path string) error {
	binding, bound, err := LoadExecutionEnvironment(path)
	if err != nil || !bound {
		return err
	}
	end, err := s.WriteAuthority().BeginSave(path)
	if err != nil {
		return err
	}
	defer end()
	return bindExecutionEnvironmentLocked(path, binding)
}

func (s *Session) beginEnvironmentSave(path string) (func(), error) {
	end, err := s.requireWriteAuthorityForSave(path)
	if err != nil {
		return nil, err
	}
	if err := s.admitEnvironmentSave(path); err != nil {
		end()
		return nil, err
	}
	return end, nil
}

func replayEnvironmentHeader(path string, rec sessionEventWireRecord, seen *bool, records int) (bool, error) {
	if rec.SchemaVersion != sessionEnvironmentSchemaVersion && rec.Type != sessionEnvironmentEventType {
		return false, nil
	}
	var binding ExecutionEnvironmentBinding
	if *seen || records != 0 || rec.SchemaVersion != sessionEnvironmentSchemaVersion || rec.Type != sessionEnvironmentEventType ||
		json.Unmarshal(rec.Binding, &binding) != nil || !validEnvironmentBinding(store.SessionPathForEventLog(path), binding) {
		return false, ErrExecutionEnvironmentProtocol
	}
	*seen = true
	return true, nil
}

func refuseBoundConversationTransfer(path string) error {
	if _, bound, err := LoadExecutionEnvironment(path); err != nil {
		return err
	} else if bound {
		return ErrExecutionEnvironmentMismatch
	}
	return nil
}

// EnsureExecutionEnvironmentFence upgrades a sidecar-only experimental binding
// only while the execution owner holds the transcript's current authority.
func EnsureExecutionEnvironmentFence(path string, authority *SessionWriteAuthority) error {
	if _, _, fenced, err := readEnvironmentFence(path); err != nil || fenced {
		return err
	}
	binding, bound, err := LoadExecutionEnvironment(path)
	if err != nil || !bound {
		return err
	}
	return BindExecutionEnvironment(path, binding, authority)
}

func environmentContentSchema(path string) (int, error) {
	raw, _, fenced, err := readEnvironmentFence(path)
	if err != nil || !fenced {
		return 0, err
	}
	f, err := os.Open(SessionEventLogPath(path))
	if err != nil {
		return 0, err
	}
	defer f.Close()
	if _, err := f.Seek(int64(len(raw)), io.SeekStart); err != nil {
		return 0, err
	}
	schema, _, ok := probeSessionEventHeader(f)
	if !ok {
		return sessionEventSchemaVersion, nil
	}
	return schema, nil
}
