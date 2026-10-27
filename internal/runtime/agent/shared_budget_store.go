package agent

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"reasonix/internal/base/filelock"
	"reasonix/internal/contract/pricing"
	"reasonix/internal/state/store"
	"time"
)

type budgetReservation struct {
	Tokens   int            `json:"tokens"`
	Cost     pricing.Amount `json:"cost"`
	Currency string         `json:"currency"`
}

type sharedBudgetRecord struct {
	Version  int                          `json:"version"`
	Scope    string                       `json:"scope"`
	Limit    TaskBudget                   `json:"limit"`
	Started  time.Time                    `json:"started"`
	Tokens   int                          `json:"tokens"`
	Currency string                       `json:"currency"`
	Cost     map[string]pricing.Amount    `json:"cost"`
	Reserved map[string]budgetReservation `json:"reserved"`
}

func budgetLedgerPath(session, scope string) string {
	if session == "" || scope == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(scope))
	return filepath.Join(store.SessionBudgetDir(session), hex.EncodeToString(sum[:])+".json")
}

func (b *sharedBudget) transact(ctx context.Context, edit func() error) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.fault != nil {
		return b.fault
	}
	if b.path == "" {
		return edit()
	}
	if err := os.MkdirAll(filepath.Dir(b.path), 0700); err != nil {
		return err
	}
	info, err := os.Lstat(filepath.Dir(b.path))
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("invalid task budget directory")
	}
	root, err := os.OpenRoot(filepath.Dir(b.path))
	if err != nil {
		return err
	}
	defer root.Close()
	release, err := filelock.AcquireRoot(ctx, root, filepath.Base(b.path)+".lock")
	if err != nil {
		return err
	}
	defer release()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := b.loadLocked(root); err != nil {
		return err
	}
	if err := edit(); err != nil {
		return err
	}
	r := sharedBudgetRecord{1, b.scope, b.limit, b.started, b.tokens, b.currency, b.cost, b.reserved}
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if len(data) > 4<<20 {
		return fmt.Errorf("shared budget ledger exceeds limit")
	}
	if err := writeBudgetLedger(root, filepath.Base(b.path), data); err != nil {
		b.fault = err
		return err
	}
	b.requireExisting = true
	return nil
}

func (b *sharedBudget) loadLocked(root *os.Root) error {
	name := filepath.Base(b.path)
	info, err := root.Lstat(name)
	if os.IsNotExist(err) {
		if b.requireExisting {
			return fmt.Errorf("persisted task budget is missing; explicit budget renewal is required")
		}
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 4<<20 {
		return fmt.Errorf("invalid shared budget ledger")
	}
	f, err := root.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (4<<20)+1))
	if err != nil {
		return err
	}
	var r sharedBudgetRecord
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if len(data) > 4<<20 || d.Decode(&r) != nil || d.Decode(new(any)) != io.EOF || r.Version != 1 || r.Scope != b.scope || r.Tokens < 0 || r.Started.IsZero() || r.Cost == nil || r.Reserved == nil || normalizeTaskBudget(r.Limit) != r.Limit || math.IsInf(r.Limit.Cost, 0) || math.IsNaN(r.Limit.Cost) {
		return fmt.Errorf("unsupported or malformed shared budget ledger; preserved")
	}
	for _, hold := range r.Reserved {
		if hold.Tokens < 0 || hold.Cost < 0 {
			return fmt.Errorf("invalid budget reservation")
		}
	}
	for _, cost := range r.Cost {
		if cost < 0 {
			return fmt.Errorf("invalid budget spend")
		}
	}
	b.limit, b.started, b.tokens, b.currency, b.cost, b.reserved = r.Limit, r.Started, r.Tokens, r.Currency, r.Cost, r.Reserved
	return nil
}

func writeBudgetLedger(root *os.Root, name string, data []byte) error {
	tmp := name + ".tmp-" + rand.Text()
	f, err := root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = root.Remove(tmp) }()
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = root.Rename(tmp, name); err != nil {
		return err
	}
	if dir, err := root.Open("."); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}
