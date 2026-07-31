package runtime

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"reasonix/internal/autoresearch"
	"reasonix/internal/fileutil"
	"reasonix/internal/store"
)

const (
	profileTransactionVersion   = 1
	profileTransactionPrepared  = "prepared"
	profileTransactionCommitted = "committed"
)

// profileTransactionJournal is a rollback record for the durable side effects
// of a Profile+Goal update. A prepared journal restores the old files and
// removes a task carrying AutoResearchCreateToken; a committed journal proves
// every replacement completed and can be removed.
type profileTransactionJournal struct {
	Version                 int    `json:"version"`
	Workspace               string `json:"workspace"`
	Phase                   string `json:"phase"`
	SessionPath             string `json:"sessionPath"`
	RegistryExisted         bool   `json:"registryExisted"`
	RegistryContents        []byte `json:"registryContents,omitempty"`
	GoalExisted             bool   `json:"goalExisted"`
	GoalContents            []byte `json:"goalContents,omitempty"`
	AutoResearchCreateToken string `json:"autoResearchCreateToken,omitempty"`
}

type profileGoalTransaction struct {
	path    string
	journal profileTransactionJournal
}

func profileTransactionCreateToken(txn *profileGoalTransaction) string {
	if txn == nil {
		return ""
	}
	return txn.journal.AutoResearchCreateToken
}

func (s *Server) profileTransactionPath() string {
	registryPath := s.sessionRegistryPath()
	if registryPath == "" {
		return ""
	}
	return registryPath + ".profile-transaction.json"
}

func readTransactionPreimage(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		return data, true, nil
	}
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	return nil, false, err
}

func writeProfileTransactionJournal(path string, journal profileTransactionJournal) error {
	body, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(path, append(body, '\n'), 0o600)
}

func newProfileTransactionCreateToken() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate profile transaction create token: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func (s *Server) beginProfileGoalTransaction(sessionPath string) (*profileGoalTransaction, error) {
	journalPath := s.profileTransactionPath()
	if journalPath == "" {
		return nil, nil
	}
	if err := s.recoverProfileTransaction(); err != nil {
		return nil, err
	}
	registryPath := s.sessionRegistryPath()
	registry, registryExisted, err := readTransactionPreimage(registryPath)
	if err != nil {
		return nil, fmt.Errorf("read profile transaction registry preimage: %w", err)
	}
	sessionPath, err = containedSessionPath(s.sessionDir(), sessionPath)
	if err != nil {
		return nil, fmt.Errorf("resolve profile transaction session: %w", err)
	}
	goal, goalExisted, err := readTransactionPreimage(store.SessionGoalState(sessionPath))
	if err != nil {
		return nil, fmt.Errorf("read profile transaction Goal preimage: %w", err)
	}
	createToken, err := newProfileTransactionCreateToken()
	if err != nil {
		return nil, err
	}
	journal := profileTransactionJournal{
		Version: profileTransactionVersion, Workspace: canonicalRegistryWorkspace(s.opts.Workspace),
		Phase: profileTransactionPrepared, SessionPath: sessionPath,
		RegistryExisted: registryExisted, RegistryContents: registry,
		GoalExisted: goalExisted, GoalContents: goal,
		AutoResearchCreateToken: createToken,
	}
	if err := writeProfileTransactionJournal(journalPath, journal); err != nil {
		return nil, fmt.Errorf("prepare profile transaction: %w", err)
	}
	return &profileGoalTransaction{path: journalPath, journal: journal}, nil
}

func (s *Server) commitProfileGoalTransaction(txn *profileGoalTransaction) error {
	if txn == nil {
		return nil
	}
	txn.journal.Phase = profileTransactionCommitted
	if err := writeProfileTransactionJournal(txn.path, txn.journal); err == nil {
		return nil
	} else if removeErr := os.Remove(txn.path); removeErr == nil || os.IsNotExist(removeErr) {
		// Both durable replacements already succeeded. Removing the prepared
		// journal is an equivalent commit marker when rewriting it is impossible.
		return nil
	} else {
		return fmt.Errorf("commit profile transaction: %w (remove prepared journal: %v)", err, removeErr)
	}
}

func finishProfileGoalTransaction(txn *profileGoalTransaction) error {
	if txn == nil {
		return nil
	}
	if err := os.Remove(txn.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func restoreTransactionPreimage(path string, existed bool, data []byte, perm os.FileMode) error {
	if existed {
		return fileutil.AtomicWriteFile(path, data, perm)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *Server) rollbackProfileGoalTransaction(txn *profileGoalTransaction) error {
	if txn == nil {
		return nil
	}
	if err := restoreTransactionPreimage(store.SessionGoalState(txn.journal.SessionPath), txn.journal.GoalExisted, txn.journal.GoalContents, 0o644); err != nil {
		return fmt.Errorf("restore profile transaction Goal: %w", err)
	}
	if err := restoreTransactionPreimage(s.sessionRegistryPath(), txn.journal.RegistryExisted, txn.journal.RegistryContents, 0o600); err != nil {
		return fmt.Errorf("restore profile transaction registry: %w", err)
	}
	if txn.journal.AutoResearchCreateToken != "" {
		autoResearch := autoresearch.NewStore(s.opts.Workspace)
		if err := autoResearch.RemoveTaskByCreateToken(txn.journal.AutoResearchCreateToken); err != nil {
			return fmt.Errorf("restore profile transaction AutoResearch task: %w", err)
		}
	}
	return finishProfileGoalTransaction(txn)
}

func (s *Server) recoverProfileTransaction() error {
	journalPath := s.profileTransactionPath()
	if journalPath == "" {
		return nil
	}
	body, err := os.ReadFile(journalPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read profile transaction journal: %w", err)
	}
	var journal profileTransactionJournal
	if err := json.Unmarshal(body, &journal); err != nil {
		return fmt.Errorf("decode profile transaction journal: %w", err)
	}
	if journal.Version != profileTransactionVersion || !sameRegistryWorkspace(journal.Workspace, s.opts.Workspace) {
		return fmt.Errorf("profile transaction journal does not belong to this runtime")
	}
	sessionPath, err := containedSessionPath(s.sessionDir(), journal.SessionPath)
	if err != nil || sessionPath != journal.SessionPath {
		return fmt.Errorf("profile transaction journal has invalid session path")
	}
	if journal.RegistryExisted {
		var registry runtimeSessionRegistry
		if err := json.Unmarshal(journal.RegistryContents, &registry); err != nil || registry.Version != runtimeSessionRegistryVersion || !sameRegistryWorkspace(registry.Workspace, s.opts.Workspace) {
			return fmt.Errorf("profile transaction journal has invalid registry preimage")
		}
	}
	switch journal.Phase {
	case profileTransactionPrepared:
		txn := &profileGoalTransaction{path: journalPath, journal: journal}
		return s.rollbackProfileGoalTransaction(txn)
	case profileTransactionCommitted:
		return finishProfileGoalTransaction(&profileGoalTransaction{path: journalPath, journal: journal})
	default:
		return fmt.Errorf("profile transaction journal has invalid phase %q", journal.Phase)
	}
}
