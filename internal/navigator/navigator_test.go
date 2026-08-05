package navigator

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- StateGraph ---

func TestStateGraphAddAndGet(t *testing.T) {
	g := NewStateGraph()
	root := StateSnapshot{Step: 0, ParentStep: -1, Action: ""}
	child := StateSnapshot{Step: 1, ParentStep: 0, Action: "read /etc/hosts"}
	g.Add(root)
	g.Add(child)
	got, ok := g.Get(1)
	if !ok {
		t.Fatal("expected step 1 to exist")
	}
	if got.Action != "read /etc/hosts" {
		t.Errorf("expected action 'read /etc/hosts', got %q", got.Action)
	}
}

func TestStateGraphChildren(t *testing.T) {
	g := NewStateGraph()
	g.Add(StateSnapshot{Step: 0, ParentStep: -1})
	g.Add(StateSnapshot{Step: 1, ParentStep: 0, Action: "read a"})
	g.Add(StateSnapshot{Step: 2, ParentStep: 0, Action: "read b"})
	kids := g.Children(0)
	if len(kids) != 2 {
		t.Fatalf("expected 2 children, got %d", len(kids))
	}
}

func TestStateGraphLatestStep(t *testing.T) {
	g := NewStateGraph()
	if g.LatestStep() != -1 {
		t.Fatal("empty graph should have LatestStep -1")
	}
	g.Add(StateSnapshot{Step: 0, ParentStep: -1})
	g.Add(StateSnapshot{Step: 5, ParentStep: 0})
	if g.LatestStep() != 5 {
		t.Errorf("expected LatestStep 5, got %d", g.LatestStep())
	}
}

// --- StateHistory ---

func TestStateHistoryAppendAndRewind(t *testing.T) {
	h := NewStateHistory(10)
	h.Append(StateSnapshot{Step: 0, ParentStep: -1})
	h.Append(StateSnapshot{Step: 1, ParentStep: 0})
	h.Append(StateSnapshot{Step: 2, ParentStep: 1})
	snap, ok := h.Rewind(1)
	if !ok {
		t.Fatal("rewind to step 1 failed")
	}
	if snap.Step != 1 {
		t.Errorf("expected rewound step 1, got %d", snap.Step)
	}
	latest, _ := h.Latest()
	if latest.Step != 1 {
		t.Errorf("after rewind, latest should be 1, got %d", latest.Step)
	}
}

func TestStateHistoryWindowEviction(t *testing.T) {
	h := NewStateHistory(3)
	h.Append(StateSnapshot{Step: 0, ParentStep: -1}) // root, never evicted
	for i := 1; i <= 10; i++ {
		h.Append(StateSnapshot{Step: i, ParentStep: i - 1})
	}
	all := h.All()
	if len(all) > 3 {
		t.Errorf("expected at most 3 entries, got %d", len(all))
	}
	if all[0].Step != 0 {
		t.Error("root should never be evicted")
	}
}

// --- ContinuousStateManager ---

func TestStateManagerSeedAndAfterAction(t *testing.T) {
	m := NewContinuousStateManager(50)
	ctx := context.Background()
	m.Seed(ctx, "iface-abc", "env-xyz")
	latest, ok := m.History().Latest()
	if !ok || latest.Step != 0 {
		t.Fatal("seed should create step 0")
	}
	predicted := m.BeforeAction(ctx, "read /app/config.yaml")
	if predicted.Step != 1 {
		t.Errorf("predicted step should be 1, got %d", predicted.Step)
	}
	// After action with a recovered file path.
	recovered := []Fact{
		{Key: "path:/app/config.yaml", Value: "/app/config.yaml", Source: "read result", Kind: StateKindImplicit},
	}
	obs := StateSnapshot{InterfaceHash: "iface-abc", EnvHash: "env-xyz"}
	updated := m.AfterAction(ctx, "read /app/config.yaml", obs, recovered)
	if updated.Step != 1 {
		t.Errorf("updated step should be 1, got %d", updated.Step)
	}
	facts := m.ImplicitFacts()
	if len(facts) != 1 {
		t.Fatalf("expected 1 implicit fact, got %d", len(facts))
	}
	if facts[0].Key != "path:/app/config.yaml" {
		t.Errorf("unexpected fact key: %s", facts[0].Key)
	}
}

func TestStateManagerUpsertDedup(t *testing.T) {
	m := NewContinuousStateManager(50)
	ctx := context.Background()
	m.Seed(ctx, "", "")
	m.AfterAction(ctx, "read f", StateSnapshot{}, []Fact{
		{Key: "id:user-42", Value: "user-42", Kind: StateKindImplicit},
	})
	// Same key, different value — should upsert, not append.
	m.AfterAction(ctx, "read f2", StateSnapshot{}, []Fact{
		{Key: "id:user-42", Value: "user-42-renamed", Kind: StateKindImplicit},
	})
	facts := m.ImplicitFacts()
	if len(facts) != 1 {
		t.Fatalf("expected 1 fact after upsert, got %d", len(facts))
	}
	if facts[0].Value != "user-42-renamed" {
		t.Errorf("expected upserted value, got %s", facts[0].Value)
	}
}

func TestStateManagerImplicitStateDigest(t *testing.T) {
	m := NewContinuousStateManager(50)
	ctx := context.Background()
	m.Seed(ctx, "", "")
	m.AfterAction(ctx, "read f", StateSnapshot{}, []Fact{
		{Key: "path:/etc/hosts", Value: "/etc/hosts", Source: "read", Kind: StateKindImplicit},
		{Key: "id:svc-7", Value: "svc-7", Source: "read", Kind: StateKindImplicit},
	})
	digest := m.ImplicitStateDigest()
	if !strings.Contains(digest, "/etc/hosts") {
		t.Errorf("digest should contain the path, got: %s", digest)
	}
	if !strings.Contains(digest, "svc-7") {
		t.Errorf("digest should contain the id, got: %s", digest)
	}
}

func TestStateManagerCarriesFactsForward(t *testing.T) {
	m := NewContinuousStateManager(50)
	ctx := context.Background()
	m.Seed(ctx, "", "")
	m.AfterAction(ctx, "read a", StateSnapshot{}, []Fact{
		{Key: "path:/a", Value: "/a", Kind: StateKindImplicit},
	})
	m.AfterAction(ctx, "read b", StateSnapshot{}, []Fact{
		{Key: "path:/b", Value: "/b", Kind: StateKindImplicit},
	})
	// After two actions, BOTH facts should be present — the core defense
	// against implicit-state amnesia.
	facts := m.ImplicitFacts()
	if len(facts) != 2 {
		t.Fatalf("expected 2 accumulated facts, got %d", len(facts))
	}
}

// --- Comparator ---

func TestCompareNoDeviation(t *testing.T) {
	pred := StateSnapshot{InterfaceHash: "abc", EnvHash: "xyz", ImplicitFacts: []Fact{{Key: "k1"}}}
	obs := StateSnapshot{InterfaceHash: "abc", EnvHash: "xyz", ImplicitFacts: []Fact{{Key: "k1"}}}
	d := Compare(pred, obs)
	if d.Kind != DeviationNone {
		t.Errorf("expected no deviation, got %v: %s", d.Kind, d.Message)
	}
}

func TestCompareFactLost(t *testing.T) {
	pred := StateSnapshot{InterfaceHash: "abc", EnvHash: "xyz", ImplicitFacts: []Fact{{Key: "k1"}, {Key: "k2"}}}
	obs := StateSnapshot{InterfaceHash: "abc", EnvHash: "xyz", ImplicitFacts: []Fact{{Key: "k1"}}}
	d := Compare(pred, obs)
	if d.Kind != DeviationFactLost {
		t.Errorf("expected DeviationFactLost, got %v", d.Kind)
	}
	if len(d.MissingFactKeys) != 1 || d.MissingFactKeys[0] != "k2" {
		t.Errorf("expected missing key k2, got %v", d.MissingFactKeys)
	}
}

func TestCompareInterfaceDrift(t *testing.T) {
	// Prediction says interface shouldn't change (non-empty hash); observation
	// differs → interface drift.
	pred := StateSnapshot{InterfaceHash: "abc", EnvHash: "xyz", Action: "read f"}
	obs := StateSnapshot{InterfaceHash: "def", EnvHash: "xyz"}
	d := Compare(pred, obs)
	if d.Kind != DeviationInterfaceDrift {
		t.Errorf("expected DeviationInterfaceDrift, got %v: %s", d.Kind, d.Message)
	}
}

func TestCompareTotalMismatch(t *testing.T) {
	pred := StateSnapshot{InterfaceHash: "abc", EnvHash: "xyz", Action: "exec cmd"}
	obs := StateSnapshot{InterfaceHash: "def", EnvHash: "uvw"}
	d := Compare(pred, obs)
	if d.Kind != DeviationTotalMismatch {
		t.Errorf("expected DeviationTotalMismatch, got %v: %s", d.Kind, d.Message)
	}
}

// --- ClosedLoopEngine ---

func TestEngineDecideContinue(t *testing.T) {
	e := NewClosedLoopEngine()
	c := e.Decide("read f", Deviation{Kind: DeviationNone}, -1)
	if c.Strategy != StrategyContinue {
		t.Errorf("expected StrategyContinue, got %v", c.Strategy)
	}
}

func TestEngineDecideReinjectFacts(t *testing.T) {
	e := NewClosedLoopEngine()
	d := Deviation{Kind: DeviationFactLost, MissingFactKeys: []string{"path:/etc/hosts"}}
	c := e.Decide("read f", d, -1)
	if c.Strategy != StrategyReinjectFacts {
		t.Errorf("expected StrategyReinjectFacts, got %v", c.Strategy)
	}
	if len(c.Reinject) != 1 {
		t.Errorf("expected 1 reinject fact, got %d", len(c.Reinject))
	}
}

func TestEngineDecideRetryThenRollback(t *testing.T) {
	e := NewClosedLoopEngine()
	action := "exec cmd"
	d := Deviation{Kind: DeviationInterfaceDrift, Message: "drift"}
	// First few attempts should retry.
	for i := 0; i < MaxRetries; i++ {
		c := e.Decide(action, d, 5)
		if c.Strategy != StrategyRetry {
			t.Errorf("attempt %d: expected StrategyRetry, got %v", i, c.Strategy)
		}
	}
	// After MaxRetries, should roll back to lastGoodStep.
	c := e.Decide(action, d, 5)
	if c.Strategy != StrategyRollback {
		t.Errorf("after max retries, expected StrategyRollback, got %v", c.Strategy)
	}
	if c.RewindTo != 5 {
		t.Errorf("expected rewind to step 5, got %d", c.RewindTo)
	}
}

func TestEngineDecideAskHostWhenNoRollback(t *testing.T) {
	e := NewClosedLoopEngine()
	action := "exec cmd"
	d := Deviation{Kind: DeviationTotalMismatch, Message: "mismatch"}
	for i := 0; i < MaxRetries; i++ {
		e.Decide(action, d, -1) // no known-good step
	}
	c := e.Decide(action, d, -1)
	if c.Strategy != StrategyAskHost {
		t.Errorf("expected StrategyAskHost when no rollback target, got %v", c.Strategy)
	}
}

// --- ExtractImplicitFacts ---

func TestExtractImplicitFactsPaths(t *testing.T) {
	result := "Found matches:\n/some/path/main.go:42: TODO\n/another/pkg/util.go:15: TODO"
	facts := ExtractImplicitFacts("grep", result, nil)
	found := false
	for _, f := range facts {
		if strings.Contains(f.Value, "/some/path/main.go") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected to extract path /some/path/main.go, got %v", facts)
	}
}

func TestExtractImplicitFactsWindowsPaths(t *testing.T) {
	result := `Output: D:\projects\src\main.rs
Compiled C:\Users\test\app.exe`
	facts := ExtractImplicitFacts("exec", result, nil)
	found := false
	for _, f := range facts {
		if strings.Contains(f.Value, `D:\projects\src\main.rs`) {
			found = true
		}
	}
	if !found {
		t.Errorf("expected to extract Windows path, got %v", facts)
	}
}

func TestExtractImplicitFactsError(t *testing.T) {
	err := errors.New("permission denied: /root/secret")
	facts := ExtractImplicitFacts("read", "", err)
	if len(facts) != 1 {
		t.Fatalf("expected 1 fact (error), got %d", len(facts))
	}
	if !strings.Contains(facts[0].Value, "permission denied") {
		t.Errorf("expected error fact, got %v", facts[0])
	}
}

// --- Sensors ---

func TestFilesystemSensorDetectsChanges(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	s := NewFilesystemSensor(dir, 3)
	// First snapshot: empty.
	hash1, _, err := s.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Create a file.
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	hash2, _, err := s.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if hash1 == hash2 {
		t.Error("expected hash to change after file creation")
	}
	changes := s.Changes()
	if len(changes) == 0 {
		t.Error("expected at least one change event")
	}
}

func TestEventCorrelatorPromotesCrossSource(t *testing.T) {
	c := NewEventCorrelator(100)
	now := time.Now()
	c.Ingest([]SensorEvent{
		{Source: "filesystem", Kind: "create", At: now, Severity: 0},
		{Source: "process", Kind: "appear", At: now.Add(10 * time.Millisecond), Severity: 0},
	})
	flushed := c.Flush()
	if len(flushed) != 1 {
		t.Fatalf("expected 1 correlated batch, got %d", len(flushed))
	}
	if flushed[0].Severity != 2 {
		t.Errorf("cross-source batch should be promoted to severity 2, got %d", flushed[0].Severity)
	}
}

// --- End-to-end Navigator with mock adapter ---

// mockAdapter is a test HostAdapter that records calls and returns canned results.
type mockAdapter struct {
	mu          sync.Mutex
	execCount   int
	outputs     []string
	errs        []error
	permAllow   bool
	permReason  string
	ifaceProbes []string
	envProbes   []string
	events      []HostEvent
}

func (m *mockAdapter) Execute(ctx context.Context, action HostAction) (HostResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := m.execCount
	m.execCount++
	var out string
	var err error
	if idx < len(m.outputs) {
		out = m.outputs[idx]
	}
	if idx < len(m.errs) {
		err = m.errs[idx]
	}
	return HostResult{Output: out, Err: err}, err
}

func (m *mockAdapter) Permission(ctx context.Context, action HostAction) (bool, string) {
	return m.permAllow, m.permReason
}

func (m *mockAdapter) Emit(ctx context.Context, ev HostEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, ev)
}

func (m *mockAdapter) InterfaceProbe(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := m.execCount - 1
	if idx >= 0 && idx < len(m.ifaceProbes) {
		return m.ifaceProbes[idx], nil
	}
	return "", nil
}

func (m *mockAdapter) SnapshotEnv(ctx context.Context) (string, error) {
	return "", nil
}

func TestNavigatorEndToEndNoDeviation(t *testing.T) {
	ctx := context.Background()
	adapter := &mockAdapter{permAllow: true}
	n := New(adapter, Options{HistoryWindow: 20})
	if err := n.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	adapter.outputs = []string{"file contents here"}
	_, corr, err := n.Execute(ctx, HostAction{Verb: "read", Target: "/app/config.yaml", Args: `{"file_path":"/app/config.yaml"}`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if corr.Strategy != StrategyContinue {
		t.Errorf("expected StrategyContinue, got %v: %s", corr.Strategy, corr.Reason)
	}
}

func TestNavigatorEndToEndFactRecovery(t *testing.T) {
	ctx := context.Background()
	adapter := &mockAdapter{permAllow: true}
	n := New(adapter, Options{HistoryWindow: 20})
	n.Seed(ctx)
	// Action returns a result containing a file path — the navigator should
	// recover it as an implicit fact.
	adapter.outputs = []string{"Result: /var/log/app.log has 42 entries"}
	_, corr, err := n.Execute(ctx, HostAction{Verb: "read", Args: `{"file_path":"/var/log"}`})
	if err != nil {
		t.Fatal(err)
	}
	if corr.Strategy != StrategyContinue {
		t.Errorf("expected continue, got %v", corr.Strategy)
	}
	facts := n.StateManager().ImplicitFacts()
	if len(facts) == 0 {
		t.Error("expected implicit facts to be recovered from the result")
	}
	digest := n.StateManager().ImplicitStateDigest()
	if !strings.Contains(digest, "/var/log/app.log") {
		t.Errorf("digest should contain recovered path, got: %s", digest)
	}
}

func TestNavigatorPermissionDenied(t *testing.T) {
	ctx := context.Background()
	adapter := &mockAdapter{permAllow: false, permReason: "blocked by deny rule"}
	n := New(adapter, Options{})
	n.Seed(ctx)
	_, corr, err := n.Execute(ctx, HostAction{Verb: "write", Args: `{}`})
	if !errors.Is(err, ErrAskHost) {
		t.Errorf("expected ErrAskHost on permission denied, got %v", err)
	}
	if corr.Strategy != StrategyAskHost {
		t.Errorf("expected StrategyAskHost, got %v", corr.Strategy)
	}
}

// --- HermesAdapter ---

func TestHermesToolMapping(t *testing.T) {
	m := HermesToolMapping()
	if m["read"] != "read_file" {
		t.Errorf("expected read→read_file, got %s", m["read"])
	}
	if m["exec"] != "terminal" {
		t.Errorf("expected exec→terminal, got %s", m["exec"])
	}
}

func TestHermesAdapterFailsClosedWithoutBackend(t *testing.T) {
	ctx := context.Background()
	a := NewHermesAdapter(HermesAdapterOptions{})
	_, err := a.Execute(ctx, HostAction{Verb: "read", Args: `{}`})
	if err == nil {
		t.Error("expected error when no backend wired (fail-closed)")
	}
}

func TestHermesAdapterHookSimulation(t *testing.T) {
	ctx := context.Background()
	var preCalled, postCalled bool
	a := NewHermesAdapter(HermesAdapterOptions{
		Backend: func(ctx context.Context, tool, args string) (string, error) {
			return "ok", nil
		},
		Hooks: HermesHookSimulator{
			PreTool: func(ctx context.Context, tool, args string) (bool, string) {
				preCalled = true
				return true, ""
			},
			PostTool: func(ctx context.Context, tool, args, output string, err error) {
				postCalled = true
			},
		},
	})
	result, err := a.Execute(ctx, HostAction{Verb: "read", Args: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	if !preCalled {
		t.Error("PreTool hook should have been called")
	}
	if !postCalled {
		t.Error("PostTool hook should have been called")
	}
	if result.Output != "ok" {
		t.Errorf("expected output 'ok', got %s", result.Output)
	}
}

func TestHermesAdapterPreToolBlocks(t *testing.T) {
	ctx := context.Background()
	a := NewHermesAdapter(HermesAdapterOptions{
		Backend: func(ctx context.Context, tool, args string) (string, error) {
			t.Error("backend should not be called when PreTool blocks")
			return "", nil
		},
		Hooks: HermesHookSimulator{
			PreTool: func(ctx context.Context, tool, args string) (bool, string) {
				return false, "blocked by policy"
			},
		},
	})
	_, err := a.Execute(ctx, HostAction{Verb: "write", Args: `{}`})
	if err == nil {
		t.Error("expected error when PreTool blocks")
	}
}

// --- Concurrency ---

func TestStateManagerConcurrentAfterAction(t *testing.T) {
	m := NewContinuousStateManager(100)
	ctx := context.Background()
	m.Seed(ctx, "", "")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m.AfterAction(ctx, "read f", StateSnapshot{}, []Fact{
				{Key: "key-" + string(rune('a'+i)), Value: "v", Kind: StateKindImplicit},
			})
		}(i)
	}
	wg.Wait()
	facts := m.ImplicitFacts()
	if len(facts) != 20 {
		t.Errorf("expected 20 facts after concurrent inserts, got %d", len(facts))
	}
}
