package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/base/locktrace"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

type measuredWorker struct {
	ID              int                    `json:"id"`
	ReadyNS         int64                  `json:"preparation_ns"`
	RunNS           int64                  `json:"run_ns"`
	EpochNS         int64                  `json:"epoch_unix_ns"`
	CoordinationDir string                 `json:"coordination_dir"`
	EffectivePolicy string                 `json:"effective_policy"`
	Conflicts       int                    `json:"conflicts"`
	Reschedules     int                    `json:"reschedules"`
	Phases          []interval             `json:"phases"`
	Locks           []locktrace.Span       `json:"locks,omitempty"`
	Notices         []event.WorkspaceLease `json:"contention_events"`
}

type measureSink struct {
	mu      sync.Mutex
	clock   func() int64
	phases  []interval
	tasks   map[string]int64
	waits   map[string]int64
	notices []event.WorkspaceLease
	results map[string]event.Tool
}

func (s *measureSink) Emit(e event.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock()
	if e.Kind == event.ToolDispatch && !e.Tool.Partial {
		s.tasks[e.Tool.ID] = now
	}
	if e.Kind == event.ToolResult {
		s.results[e.Tool.ID] = e.Tool
		if started, ok := s.tasks[e.Tool.ID]; ok {
			s.phases = append(s.phases, interval{"tool_dispatch_to_result", started, now})
			delete(s.tasks, e.Tool.ID)
		}
	}
	if notice := e.WorkspaceLease; notice != nil {
		s.notices = append(s.notices, *notice)
		if notice.TransactionID != "" {
			if notice.State == "waiting" {
				if _, exists := s.waits[notice.TransactionID]; !exists {
					s.waits[notice.TransactionID] = now
				}
			} else if started, exists := s.waits[notice.TransactionID]; exists {
				s.phases = append(s.phases, interval{"resource_wait", started, now})
				delete(s.waits, notice.TransactionID)
			}
		}
	}
}

func (s *measureSink) result(id string) (string, bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.results[id]
	return r.RefusalCode, r.Err == "", ok
}

func runWorker(ctx context.Context) (err error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	decoder, encoder := json.NewDecoder(os.Stdin), json.NewEncoder(os.Stdout)
	var r request
	if err := decoder.Decode(&r); err != nil {
		return err
	}
	epoch := time.Now()
	clock := func() int64 { return time.Since(epoch).Nanoseconds() }
	var collector *locktrace.Collector
	if r.Load.Record {
		var stop func()
		collector, stop, err = locktrace.Start()
		if err != nil {
			return err
		}
		defer stop()
		clock = collector.Elapsed
	}
	sink := &measureSink{clock: clock, tasks: map[string]int64{}, waits: map[string]int64{}, results: map[string]event.Tool{}}
	p := &simulatedProvider{request: r, clock: clock, result: sink.result}
	resolver := &provider.StaticResolver{Descriptors: []provider.Descriptor{{Ref: "probe/scripted", DisplayName: "probe", Model: "scripted", ContextWindow: 128000, Tools: true}}, Providers: map[string]provider.Provider{"probe/scripted": p}}
	ctrl, err := boot.Build(ctx, boot.Options{Home: r.Home, WorkspaceRoot: r.Root, SessionDir: r.Sessions, Model: "probe/scripted", ProviderResolver: resolver, Sink: sink, Stderr: io.Discard, HeadlessApprovalMode: control.ToolApprovalYolo})
	if err != nil {
		return err
	}
	defer ctrl.Close()
	policy := ctrl.WriteLeaseSettings().Effective
	if policy == nil || policy.Collaboration != r.Mode {
		return fmt.Errorf("requested policy %s was not assembled: %+v", r.Mode, policy)
	}
	ctrl.ApplyHeadlessApprovalMode(control.ToolApprovalYolo)
	ctrl.EnsureSessionPath()
	lease, err := sessionstore.TryAcquireSessionLease(ctrl.SessionPath())
	if err != nil {
		return err
	}
	defer lease.Release()
	if err := ctrl.BindSessionWriteAuthority(lease); err != nil {
		return err
	}
	result := measuredWorker{ID: r.ID, EpochNS: epoch.UnixNano(), ReadyNS: clock(), CoordinationDir: config.WorkspaceLeaseDir(), EffectivePolicy: policy.Collaboration}
	if collector != nil {
		result.EpochNS = collector.EpochUnixNano()
	}
	if err := encoder.Encode("ready"); err != nil {
		return err
	}
	var start string
	if err := decoder.Decode(&start); err != nil || start != "start" {
		return errors.Join(err, errors.New("start barrier missing"))
	}
	go func() {
		var message json.RawMessage
		_ = decoder.Decode(&message)
		cancel()
	}()
	started := clock()
	if err := ctrl.Run(ctx, "Perform the fixed file mutation fixture"); err != nil {
		return err
	}
	if err := ctrl.Snapshot(); err != nil {
		return err
	}
	ctrl.Close()
	lease.Release()
	result.RunNS = clock() - started
	sink.mu.Lock()
	if len(sink.tasks) != 0 || len(sink.waits) != 0 {
		sink.mu.Unlock()
		return errors.New("unclosed product tool or wait event")
	}
	result.Phases = append(result.Phases, sink.phases...)
	result.Notices = append(result.Notices, sink.notices...)
	sink.mu.Unlock()
	result.Phases = append(result.Phases, p.phases...)
	result.Conflicts, result.Reschedules = p.conflicts, p.reschedules
	if p.step != r.Load.Steps {
		return fmt.Errorf("worker completed %d/%d mutations", p.step, r.Load.Steps)
	}
	if collector != nil {
		var complete bool
		result.Locks, complete = collector.Snapshot()
		if !complete {
			return errors.New("OS lock trace overflow")
		}
		for _, span := range result.Locks {
			if span.ReleasedNS == 0 {
				return fmt.Errorf("OS hold remains open: %s", span.Path)
			}
		}
	}
	return encoder.Encode(result)
}
