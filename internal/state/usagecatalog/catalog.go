// Package usagecatalog maintains a disposable aggregate projection of the
// authoritative daily statistics JSONL files.
package usagecatalog

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/state/projectiondb"
)

// SchemaVersion names the projection file, so a bump retires the old one
// rather than migrating it: every column here is replayable from the JSONL.
const SchemaVersion = 5

type AppendReceipt struct {
	Path     string
	Day      string
	Offset   int64
	Length   int
	LineHash string
}

type Entry struct {
	Day        string
	Source     string
	ModelRef   string
	Provider   string
	Prompt     int
	Completion int
	Reasoning  int
	CacheHit   int
	CacheMiss  int
	Total      int
	Requests   int
	Turns      int
	// Cost is pricing.Amount fixed-point, never a float. CostCurrency is empty
	// on rows written before cost was recorded, which is not the same as zero.
	Cost          int64
	CostCurrency  string
	CostEstimated bool
	Valuations    map[string]int64 // vendor-table amounts by currency, fixed-point
}

// CostAmount is one currency's total. Currencies never add, so they never share
// a column with each other or with the token counts.
type CostAmount struct {
	Currency string
	Amount   int64
	// Estimated is set when any row folded in here was priced from a fallback
	// rate card rather than a provider-reported amount.
	Estimated bool
}

type Rollup struct {
	Day        string
	Source     string
	ModelRef   string
	Provider   string
	Prompt     int64
	Completion int64
	Reasoning  int64
	CacheHit   int64
	CacheMiss  int64
	Total      int64
	Requests   int64
	Turns      int64
	Costs      []CostAmount
}

type Status struct {
	State        string            `json:"state"`
	Mode         projectiondb.Mode `json:"mode"`
	Path         string            `json:"path,omitempty"`
	Revision     uint64            `json:"revision"`
	IndexedFiles int64             `json:"indexedFiles"`
	LagBytes     int64             `json:"lagBytes"`
	CorruptLines int64             `json:"corruptLines"`
	Fallbacks    uint64            `json:"fallbacks"`
	LastError    string            `json:"lastError,omitempty"`
}

type Catalog struct {
	db         *sql.DB
	statusMu   sync.RWMutex
	status     Status
	revision   atomic.Uint64
	fallback   atomic.Uint64
	queue      chan receiptEntry
	dirtyFiles sync.Map
	dirtyDirs  sync.Map
	dirtyWake  chan struct{}
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	closeOnce  sync.Once
	closeDone  chan struct{}
	closeErr   error
}

type receiptEntry struct {
	receipt AppendReceipt
	entry   Entry
	flush   chan struct{}
}

// DefaultPath returns the disposable usage rollup path under CacheDir.
// Empty when cache is unavailable so Open falls back to an in-memory projection.
func DefaultPath() string {
	cache := strings.TrimSpace(config.CacheDir())
	if cache == "" {
		return ""
	}
	return filepath.Join(cache, "usage-catalog", fmt.Sprintf("v%d.sqlite", SchemaVersion))
}

// retireOlderProjections deletes the versions this build no longer opens. A
// bump replaces the file rather than migrating it, so without this every bump
// leaves another multi-MB orphan in the cache directory forever.
func retireOlderProjections(current string) {
	dir := filepath.Dir(current)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	keep := filepath.Base(current)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "v") || !strings.Contains(name, ".sqlite") {
			continue
		}
		// The -wal and -shm siblings go with their database.
		if name == keep || strings.HasPrefix(name, keep+"-") {
			continue
		}
		_ = os.Remove(filepath.Join(dir, name))
	}
}

const schema = `
CREATE TABLE usage_state(id INTEGER PRIMARY KEY CHECK(id=1),revision INTEGER NOT NULL DEFAULT 0);
INSERT INTO usage_state(id,revision) VALUES(1,0);
CREATE TABLE usage_files(
 path TEXT PRIMARY KEY,day TEXT NOT NULL,size INTEGER NOT NULL DEFAULT 0,mtime_ns INTEGER NOT NULL DEFAULT 0,
 indexed_offset INTEGER NOT NULL DEFAULT 0,state TEXT NOT NULL DEFAULT 'pending',error TEXT NOT NULL DEFAULT '',
 corrupt_lines INTEGER NOT NULL DEFAULT 0,completed_at INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE usage_records(
 file_path TEXT NOT NULL,byte_offset INTEGER NOT NULL,byte_length INTEGER NOT NULL,line_hash TEXT NOT NULL,
 day TEXT NOT NULL,source TEXT NOT NULL,model_ref TEXT NOT NULL,provider TEXT NOT NULL,
 prompt INTEGER NOT NULL,completion INTEGER NOT NULL,reasoning INTEGER NOT NULL,cache_hit INTEGER NOT NULL,
 cache_miss INTEGER NOT NULL,total INTEGER NOT NULL,requests INTEGER NOT NULL,turns INTEGER NOT NULL,
 cost INTEGER NOT NULL DEFAULT 0,cost_currency TEXT NOT NULL DEFAULT '',cost_estimated INTEGER NOT NULL DEFAULT 0,
 valuation_cny INTEGER,valuation_usd INTEGER,
 PRIMARY KEY(file_path,byte_offset)
);
CREATE TABLE usage_rollups(
 day TEXT NOT NULL,source TEXT NOT NULL,model_ref TEXT NOT NULL,provider TEXT NOT NULL,
 prompt INTEGER NOT NULL,completion INTEGER NOT NULL,reasoning INTEGER NOT NULL,cache_hit INTEGER NOT NULL,
 cache_miss INTEGER NOT NULL,total INTEGER NOT NULL,requests INTEGER NOT NULL,turns INTEGER NOT NULL,
 PRIMARY KEY(day,source,model_ref)
);
CREATE TABLE usage_cost(
 day TEXT NOT NULL,source TEXT NOT NULL,model_ref TEXT NOT NULL,view TEXT NOT NULL,
 currency TEXT NOT NULL,cost INTEGER NOT NULL,estimated INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(day,source,model_ref,view,currency)
);
CREATE INDEX idx_usage_rollups_range ON usage_rollups(day,source,model_ref);
`

func migrations() []projectiondb.Migration {
	return []projectiondb.Migration{{Version: 1, Apply: func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, schema)
		return err
	}}}
}

func Open(ctx context.Context, path string) (*Catalog, error) {
	if path == "" {
		path = DefaultPath()
	}
	inMemory := strings.TrimSpace(path) == ""
	if inMemory {
		path = ""
	} else {
		retireOlderProjections(path)
	}
	handle, err := projectiondb.Open(ctx, projectiondb.OpenOptions{
		Path: path, MemoryName: "usage-catalog", Migrations: migrations(), InMemory: inMemory, MaxOpenConns: 4,
	})
	if err != nil {
		return nil, err
	}
	workerCtx, cancel := context.WithCancel(context.Background())
	c := &Catalog{db: handle.DB, queue: make(chan receiptEntry, 1024), dirtyWake: make(chan struct{}, 1), ctx: workerCtx, cancel: cancel,
		closeDone: make(chan struct{}),
		status:    Status{State: string(handle.Status.State), Mode: handle.Status.Mode, Path: handle.Status.Path, LastError: handle.Status.LastError}}
	var revision uint64
	_ = c.db.QueryRowContext(ctx, `SELECT revision FROM usage_state WHERE id=1`).Scan(&revision)
	c.revision.Store(revision)
	c.refresh(ctx)
	c.wg.Add(1)
	go c.worker()
	return c, nil
}

func (c *Catalog) Enqueue(receipt AppendReceipt, entry Entry) bool {
	if c == nil {
		return false
	}
	select {
	case c.queue <- receiptEntry{receipt: receipt, entry: entry}:
		return true
	default:
		c.dirtyFiles.Store(receipt.Path, receipt.Day)
		c.wakeDirty()
		return false
	}
}

func (c *Catalog) worker() {
	defer c.wg.Done()
	for {
		if path, day, ok := c.takeDirtyFile(); ok {
			_ = c.ReconcileFile(c.ctx, path, day)
			continue
		}
		if dir, ok := c.takeDirtyDir(); ok {
			_ = c.ReconcileDir(c.ctx, dir)
			continue
		}
		select {
		case <-c.ctx.Done():
			return
		case <-c.dirtyWake:
		case item := <-c.queue:
			if item.flush != nil {
				close(item.flush)
				continue
			}
			_ = c.applyReceipt(c.ctx, item.receipt, item.entry)
		}
	}
}

func (c *Catalog) takeDirtyFile() (string, string, bool) {
	var path, day string
	c.dirtyFiles.Range(func(key, value any) bool {
		path, _ = key.(string)
		day, _ = value.(string)
		c.dirtyFiles.Delete(key)
		return false
	})
	return path, day, path != ""
}

func (c *Catalog) takeDirtyDir() (string, bool) {
	var dir string
	c.dirtyDirs.Range(func(key, _ any) bool {
		dir, _ = key.(string)
		c.dirtyDirs.Delete(key)
		return false
	})
	return dir, dir != ""
}

func (c *Catalog) RequestReconcileDir(dir string) {
	if c != nil && strings.TrimSpace(dir) != "" {
		c.dirtyDirs.Store(filepath.Clean(dir), true)
		c.wakeDirty()
	}
}

func (c *Catalog) wakeDirty() {
	select {
	case c.dirtyWake <- struct{}{}:
	default:
	}
}

func (c *Catalog) Flush(ctx context.Context) error {
	if c == nil {
		return nil
	}
	done := make(chan struct{})
	select {
	case c.queue <- receiptEntry{flush: done}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Catalog) applyReceipt(ctx context.Context, receipt AppendReceipt, entry Entry) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	var existingHash string
	err = tx.QueryRowContext(ctx, `SELECT line_hash FROM usage_records WHERE file_path=? AND byte_offset=?`, receipt.Path, receipt.Offset).Scan(&existingHash)
	if err == nil {
		_ = tx.Rollback()
		if existingHash != receipt.LineHash {
			return c.ReconcileFile(ctx, receipt.Path, receipt.Day)
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		_ = tx.Rollback()
		return err
	}
	if err := insertRecord(ctx, tx, receipt, entry); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := projectRecord(ctx, tx, entry); err != nil {
		_ = tx.Rollback()
		return err
	}
	end := receipt.Offset + int64(receipt.Length)
	mtime := int64(0)
	if info, statErr := os.Stat(receipt.Path); statErr == nil {
		mtime = info.ModTime().UnixNano()
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO usage_files(path,day,size,mtime_ns,indexed_offset,state,completed_at) VALUES(?,?,?,?,?,'ready',?)
        ON CONFLICT(path) DO UPDATE SET day=excluded.day,size=MAX(usage_files.size,excluded.size),
        mtime_ns=CASE WHEN usage_files.indexed_offset=? THEN excluded.mtime_ns ELSE usage_files.mtime_ns END,
        indexed_offset=CASE WHEN usage_files.indexed_offset=? THEN excluded.indexed_offset ELSE usage_files.indexed_offset END,
        state=CASE WHEN usage_files.indexed_offset=? THEN 'ready' ELSE 'pending' END,completed_at=excluded.completed_at`,
		receipt.Path, receipt.Day, end, mtime, end, time.Now().UnixMilli(), receipt.Offset, receipt.Offset, receipt.Offset)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	revision, err := bump(ctx, tx)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	c.revision.Store(revision)
	c.refresh(context.Background())
	return nil
}

// insertRecord stores one row with the cost it was quoted at, so a day's
// rollups can be derived again from its records alone.
func insertRecord(ctx context.Context, tx *sql.Tx, receipt AppendReceipt, entry Entry) error {
	if entry.Total > 0 && entry.Requests <= 0 {
		entry.Requests = 1
	}
	estimated := 0
	if entry.CostEstimated {
		estimated = 1
	}
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO usage_records(file_path,byte_offset,byte_length,line_hash,day,source,
        model_ref,provider,prompt,completion,reasoning,cache_hit,cache_miss,total,requests,turns,cost,cost_currency,cost_estimated,
        valuation_cny,valuation_usd) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		receipt.Path, receipt.Offset, receipt.Length, receipt.LineHash, entry.Day, entry.Source, entry.ModelRef, entry.Provider,
		entry.Prompt, entry.Completion, entry.Reasoning, entry.CacheHit, entry.CacheMiss, entry.Total, entry.Requests, entry.Turns,
		entry.Cost, entry.CostCurrency, estimated, valuationArg(entry, "CNY"), valuationArg(entry, "USD"))
	return err
}

// projectRecord adds one new row to its day's rollup and cost. Only a row that
// was just inserted may be projected; anything else is rederived.
func projectRecord(ctx context.Context, tx *sql.Tx, entry Entry) error {
	if entry.Total > 0 && entry.Requests <= 0 {
		entry.Requests = 1
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO usage_rollups(day,source,model_ref,provider,prompt,completion,reasoning,cache_hit,
        cache_miss,total,requests,turns) VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(day,source,model_ref) DO UPDATE SET
        prompt=prompt+excluded.prompt,completion=completion+excluded.completion,reasoning=reasoning+excluded.reasoning,
        cache_hit=cache_hit+excluded.cache_hit,cache_miss=cache_miss+excluded.cache_miss,total=total+excluded.total,
        requests=requests+excluded.requests,turns=turns+excluded.turns`,
		entry.Day, entry.Source, entry.ModelRef, entry.Provider,
		entry.Prompt, entry.Completion, entry.Reasoning, entry.CacheHit, entry.CacheMiss, entry.Total, entry.Requests, entry.Turns)
	if err != nil || entry.CostCurrency == "" || entry.Cost == 0 {
		return err
	}
	estimated := 0
	if entry.CostEstimated {
		estimated = 1
	}
	for _, view := range costViews {
		currency, amount := entry.CostIn(view)
		if _, err := tx.ExecContext(ctx, `INSERT INTO usage_cost(day,source,model_ref,view,currency,cost,estimated)
        VALUES(?,?,?,?,?,?,?) ON CONFLICT(day,source,model_ref,view,currency) DO UPDATE SET cost=cost+excluded.cost,
        estimated=MAX(estimated,excluded.estimated)`,
			entry.Day, entry.Source, entry.ModelRef, view, currency, amount, estimated); err != nil {
			return err
		}
	}
	return nil
}

// rederiveDays rebuilds the rollups and costs of the given days from every
// record that remains for them, whichever file wrote it. Adding to what was
// there would count a reindexed file once per pass.
func rederiveDays(ctx context.Context, tx *sql.Tx, days []string) error {
	for _, day := range days {
		stmts := []string{
			`DELETE FROM usage_rollups WHERE day=?`,
			`DELETE FROM usage_cost WHERE day=?`,
			`INSERT INTO usage_rollups(day,source,model_ref,provider,prompt,completion,reasoning,cache_hit,cache_miss,total,requests,turns)
        SELECT day,source,model_ref,MAX(provider),SUM(prompt),SUM(completion),SUM(reasoning),SUM(cache_hit),SUM(cache_miss),
        SUM(total),SUM(requests),SUM(turns) FROM usage_records WHERE day=? GROUP BY day,source,model_ref`,
		}
		for _, view := range costViews {
			stmts = append(stmts, viewCostSelect(view))
		}
		for _, stmt := range stmts {
			if _, err := tx.ExecContext(ctx, stmt, day); err != nil {
				return err
			}
		}
	}
	return nil
}

type rawRecord struct {
	Timestamp  time.Time `json:"ts"`
	ModelRef   string    `json:"model"`
	Source     string    `json:"source"`
	Prompt     int       `json:"prompt"`
	Completion int       `json:"completion"`
	Reasoning  int       `json:"reasoning"`
	CacheHit   int       `json:"cache_hit"`
	CacheMiss  int       `json:"cache_miss"`
	Total      int       `json:"total"`
	Requests   int       `json:"requests"`
	Turn       bool      `json:"turn"`
	// The cost quote the row was written with; absent on older rows.
	CostAmount    string `json:"cost_amount"`
	CostCurrency  string `json:"cost_currency"`
	CostEstimated bool   `json:"cost_estimated"`
	ValuationCNY  string `json:"valuation_cny"`
	ValuationUSD  string `json:"valuation_usd"`
}

func providerOf(model string) string {
	if i := strings.IndexByte(model, '/'); i > 0 {
		return model[:i]
	}
	return "default"
}

func entryFromRaw(day string, raw rawRecord) Entry {
	turns := 0
	if raw.Turn {
		turns = 1
	}
	return Entry{Day: day, Source: raw.Source, ModelRef: raw.ModelRef, Provider: providerOf(raw.ModelRef), Prompt: raw.Prompt,
		Completion: raw.Completion, Reasoning: raw.Reasoning, CacheHit: raw.CacheHit, CacheMiss: raw.CacheMiss,
		Total: raw.Total, Requests: raw.Requests, Turns: turns}.WithCost(Priced{Amount: raw.CostAmount,
		Currency: raw.CostCurrency, CNY: raw.ValuationCNY, USD: raw.ValuationUSD, Estimated: raw.CostEstimated})
}

// recordDays is every day the file's records were filed under, plus the day it
// is being filed under now: both lose or gain rows in a reindex.
func recordDays(ctx context.Context, tx *sql.Tx, path, day string) ([]string, error) {
	found, err := tx.QueryContext(ctx, `SELECT DISTINCT day FROM usage_records WHERE file_path=?`, path)
	if err != nil {
		return nil, err
	}
	defer found.Close()
	days := []string{day}
	for found.Next() {
		var d string
		if err := found.Scan(&d); err != nil {
			return nil, err
		}
		if d != day {
			days = append(days, d)
		}
	}
	return days, found.Err()
}

func (c *Catalog) ReconcileFile(ctx context.Context, path, day string) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	days, err := recordDays(ctx, tx, path, day)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM usage_records WHERE file_path=?`, path); err != nil {
		_ = tx.Rollback()
		return err
	}
	reader := bufio.NewReader(f)
	offset := int64(0)
	corrupt := int64(0)
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			trimmed := strings.TrimSpace(string(line))
			if trimmed != "" {
				var raw rawRecord
				if json.Unmarshal([]byte(trimmed), &raw) != nil {
					corrupt++
				} else {
					hash := sha256.Sum256([]byte(trimmed))
					receipt := AppendReceipt{Path: path, Day: day, Offset: offset, Length: len(line), LineHash: hex.EncodeToString(hash[:])}
					if err := insertRecord(ctx, tx, receipt, entryFromRaw(day, raw)); err != nil {
						_ = tx.Rollback()
						return err
					}
				}
			}
			offset += int64(len(line))
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			_ = tx.Rollback()
			return readErr
		}
	}
	if err := rederiveDays(ctx, tx, days); err != nil {
		_ = tx.Rollback()
		return err
	}
	info, _ := os.Stat(path)
	mtime := int64(0)
	if info != nil {
		mtime = info.ModTime().UnixNano()
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO usage_files(path,day,size,mtime_ns,indexed_offset,state,error,corrupt_lines,completed_at)
        VALUES(?,?,?,?,?,'ready','',?,?) ON CONFLICT(path) DO UPDATE SET day=excluded.day,size=excluded.size,mtime_ns=excluded.mtime_ns,
        indexed_offset=excluded.indexed_offset,state='ready',error='',corrupt_lines=excluded.corrupt_lines,completed_at=excluded.completed_at`,
		path, day, offset, mtime, offset, corrupt, time.Now().UnixMilli())
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	revision, err := bump(ctx, tx)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	c.revision.Store(revision)
	c.refresh(context.Background())
	return nil
}

func (c *Catalog) ReconcileDir(ctx context.Context, dir string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		day := strings.TrimSuffix(entry.Name(), ".jsonl")
		if err := c.ReconcileFile(ctx, filepath.Join(dir, entry.Name()), day); err != nil {
			return err
		}
	}
	return nil
}

func (c *Catalog) Ready(ctx context.Context, dir string, days []string) bool {
	for _, day := range days {
		path := filepath.Join(dir, day+".jsonl")
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return false
		}
		var size, offset, mtime int64
		var state string
		if err := c.db.QueryRowContext(ctx, `SELECT size,indexed_offset,state,mtime_ns FROM usage_files WHERE path=?`, path).Scan(&size, &offset, &state, &mtime); err != nil {
			return false
		}
		// Same-size in-place rewrites must invalidate Ready so callers fall back
		// to authoritative JSONL until the catalog rescan catches up.
		if state != "ready" || size != info.Size() || offset != info.Size() || mtime != info.ModTime().UnixNano() {
			return false
		}
	}
	return true
}

// Query reads the rollups of a day range, with their costs as view reads them.
func (c *Catalog) Query(ctx context.Context, fromDay, toDay, source, view string) ([]Rollup, error) {
	args := []any{fromDay, toDay}
	where := `day>=? AND day<=?`
	if source != "" && source != "all" {
		where += ` AND source=?`
		args = append(args, source)
	}
	rows, err := c.db.QueryContext(ctx, `SELECT day,source,model_ref,provider,prompt,completion,reasoning,cache_hit,cache_miss,total,requests,turns
        FROM usage_rollups WHERE `+where+` ORDER BY day,source,model_ref`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Rollup{}
	for rows.Next() {
		var row Rollup
		if err := rows.Scan(&row.Day, &row.Source, &row.ModelRef, &row.Provider, &row.Prompt, &row.Completion, &row.Reasoning,
			&row.CacheHit, &row.CacheMiss, &row.Total, &row.Requests, &row.Turns); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return c.attachCosts(ctx, out, where+` AND view=?`, append(args, view))
}

// attachCosts folds the per-currency totals onto the rollups they belong to. A
// second query rather than a join: joining would multiply the token columns by
// however many currencies share that key.
func (c *Catalog) attachCosts(ctx context.Context, rows []Rollup, where string, args []any) ([]Rollup, error) {
	if len(rows) == 0 {
		return rows, nil
	}
	found, err := c.db.QueryContext(ctx, `SELECT day,source,model_ref,currency,cost,estimated
        FROM usage_cost WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer found.Close()
	at := make(map[string]int, len(rows))
	for i, row := range rows {
		at[costKey(row.Day, row.Source, row.ModelRef)] = i
	}
	for found.Next() {
		var day, source, model, currency string
		var amount int64
		var estimated int
		if err := found.Scan(&day, &source, &model, &currency, &amount, &estimated); err != nil {
			return nil, err
		}
		if i, ok := at[costKey(day, source, model)]; ok {
			rows[i].Costs = append(rows[i].Costs,
				CostAmount{Currency: currency, Amount: amount, Estimated: estimated != 0})
		}
	}
	return rows, found.Err()
}

func costKey(day, source, model string) string { return day + "\x00" + source + "\x00" + model }

func bump(ctx context.Context, tx *sql.Tx) (uint64, error) {
	if _, err := tx.ExecContext(ctx, `UPDATE usage_state SET revision=revision+1 WHERE id=1`); err != nil {
		return 0, err
	}
	var revision uint64
	err := tx.QueryRowContext(ctx, `SELECT revision FROM usage_state WHERE id=1`).Scan(&revision)
	return revision, err
}

func (c *Catalog) NoteFallback() { c.fallback.Add(1) }

func (c *Catalog) refresh(ctx context.Context) {
	var files, lag, corrupt int64
	_ = c.db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(MAX(size-indexed_offset,0)),0),COALESCE(SUM(corrupt_lines),0) FROM usage_files`).Scan(&files, &lag, &corrupt)
	c.statusMu.Lock()
	c.status.Revision, c.status.IndexedFiles, c.status.LagBytes, c.status.CorruptLines = c.revision.Load(), files, lag, corrupt
	c.status.Fallbacks = c.fallback.Load()
	c.statusMu.Unlock()
}

func (c *Catalog) Status() Status {
	c.statusMu.RLock()
	defer c.statusMu.RUnlock()
	status := c.status
	status.Fallbacks = c.fallback.Load()
	return status
}

func (c *Catalog) Close(ctx context.Context) error {
	if c == nil {
		return nil
	}
	c.closeOnce.Do(func() {
		c.cancel()
		go func() {
			c.wg.Wait()
			c.closeErr = c.db.Close()
			close(c.closeDone)
		}()
	})
	select {
	case <-c.closeDone:
		return c.closeErr
	case <-ctx.Done():
		return ctx.Err()
	}
}
