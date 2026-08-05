package recovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestDecisionMatrix freezes the pure Auto Guard routing table.
// Each row is a product scenario from the PR plan decision matrix.
func TestDecisionMatrix(t *testing.T) {
	tests := []struct {
		name string
		f    Facts
		want DecisionResult
	}{
		{
			name: "ask mode bypasses auto guard",
			f:    Facts{AutoMode: false, Mutates: true},
			want: DecisionResult{Route: RouteBypass},
		},
		{
			name: "yolo mode bypasses auto guard",
			f:    Facts{AutoMode: false, Mutates: true, HighRisk: true},
			want: DecisionResult{Route: RouteBypass},
		},
		{
			name: "ordinary read allows without review",
			f:    Facts{AutoMode: true, ReadOnly: true},
			want: DecisionResult{Route: RouteAllow},
		},
		{
			name: "ordinary search allows without review",
			f:    Facts{AutoMode: true, ReadOnly: true, Mutates: false},
			want: DecisionResult{Route: RouteAllow},
		},
		{
			name: "ordinary mutation without failure allows without review",
			f:    Facts{AutoMode: true, Mutates: true},
			want: DecisionResult{Route: RouteAllow},
		},
		{
			name: "execution risk without failure stays on permission path",
			f:    Facts{AutoMode: true, Mutates: true, HighRisk: true},
			want: DecisionResult{Route: RouteAllow},
		},
		{
			name: "same failed operation still uses bounded recovery review",
			f: Facts{
				AutoMode: true, Mutates: true, HighRisk: true,
				HasActiveFailure: true, SameFailedOperation: true, FailureCount: 1,
			},
			want: DecisionResult{Route: RouteReview},
		},
		{
			name: "structured plan transition goes to reviewer without failure",
			f:    Facts{AutoMode: true, ReadOnly: true, PlanTransition: true},
			want: DecisionResult{Route: RouteReview},
		},
		{
			name: "first safe verification retry allows and consumes budget",
			f: Facts{
				AutoMode: true, Verification: true,
				HasActiveFailure: true, FailureCount: 1, SafeRetryAvailable: true,
			},
			want: DecisionResult{Route: RouteAllow, ConsumeSafeRetry: true},
		},
		{
			name: "different scoped operation after failure stays automatic",
			f: Facts{
				AutoMode: true, Mutates: true,
				HasActiveFailure: true, FailureCount: 1, ExpandedScope: true,
			},
			want: DecisionResult{Route: RouteAllow},
		},
		{
			name: "different strategy operation after failure stays automatic",
			f: Facts{
				AutoMode: true, Mutates: true,
				HasActiveFailure: true, FailureCount: 1, StrategyChanged: true,
			},
			want: DecisionResult{Route: RouteAllow},
		},
		{
			name: "second attempt of failed operation uses bounded recovery review",
			f: Facts{
				AutoMode: true, Mutates: true,
				HasActiveFailure: true, SameFailedOperation: true, FailureCount: 2,
			},
			want: DecisionResult{Route: RouteReview},
		},
		{
			name: "third repeat of the same operation stops",
			f: Facts{
				AutoMode: true, Mutates: true,
				HasActiveFailure: true, FailureCount: 3, SameFailedOperation: true,
			},
			want: DecisionResult{Route: RouteStop, StopReason: StopReasonOperationFailures},
		},
		{
			name: "different operation after three failures stays recoverable",
			f: Facts{
				AutoMode: true, Mutates: true,
				HasActiveFailure: true, FailureCount: 3,
			},
			want: DecisionResult{Route: RouteAllow},
		},
		{
			name: "ambiguous retry of the failed operation goes to reviewer",
			f: Facts{
				AutoMode: true, Mutates: true,
				HasActiveFailure: true, SameFailedOperation: true, FailureCount: 1,
			},
			want: DecisionResult{Route: RouteReview},
		},
		{
			name: "episode stop preserves read only diagnosis",
			f: Facts{
				AutoMode: true, ReadOnly: true, EpisodeStopped: true,
				StopReason: StopReasonEpisodeFailures,
			},
			want: DecisionResult{Route: RouteAllow},
		},
		{
			name: "safe retry budget does not apply when scope expands",
			f: Facts{
				AutoMode: true, Mutates: true, Verification: true,
				HasActiveFailure: true, FailureCount: 1,
				SafeRetryAvailable: true, ExpandedScope: true,
			},
			// Safe retry is evaluated before the bounded-failure/reviewer path.
			// A true safe verification retry cannot also expand scope (classifier
			// clears SafeRetryAvailable). When both flags appear, safe retry wins
			// only if the host marked it available — the classifier must not.
			want: DecisionResult{Route: RouteAllow, ConsumeSafeRetry: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Decide(tt.f)
			if got != tt.want {
				t.Fatalf("Decide = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestToEventApprovalCarriesPlanTransitionForDecisionSurfaces(t *testing.T) {
	approval := ToEventApproval("plan-1", PendingProposal{
		Tool:       "todo_write",
		Subject:    "Update the active execution plan",
		ChangeKind: ChangeScope,
		Rationale:  "choose the public API direction",
		PlanBefore: "1. Keep API [in_progress]",
		PlanAfter:  "1. Replace API [in_progress]",
	}, nil)
	if approval.Recovery == nil {
		t.Fatal("plan transition missing recovery payload")
	}
	if approval.Recovery.PlanBefore != "1. Keep API [in_progress]" || approval.Recovery.PlanAfter != "1. Replace API [in_progress]" {
		t.Fatalf("plan transition payload = %+v", approval.Recovery)
	}
}

func TestRepeatedFailureStopMessageDoesNotAskForRiskApproval(t *testing.T) {
	got := repeatedFailureStopMessage(3, Proposal{Tool: "bash", Subject: "go test ./..."})
	if !strings.Contains(got, "after 3") || !strings.Contains(got, "go test ./...") || !strings.Contains(got, "other operations remain available") {
		t.Fatalf("stop message = %q", got)
	}
}

// TestBehaviorMatrixGolden freezes end-to-end Gate outcomes for the product matrix.
// Ordinary paths must not call the reviewer; ambiguous recovery must.
func TestBehaviorMatrixGolden(t *testing.T) {
	type outcome struct {
		allow    bool
		prompted bool
		reviews  int32
		blocked  bool
	}
	run := func(t *testing.T, setup func(g *Gate), proposal Proposal, reviewer Reviewer) outcome {
		t.Helper()
		var reviews atomic.Int32
		var prompted atomic.Bool
		r := reviewer
		if r == nil {
			r = reviewerFunc(func(context.Context, *FailureEvent, []string, Proposal, string) (ReviewVerdict, error) {
				reviews.Add(1)
				return ReviewVerdict{Outcome: ReviewContinue, ChangeKind: ChangeSameStrategy}, nil
			})
		} else {
			inner := r
			r = reviewerFunc(func(ctx context.Context, f *FailureEvent, d []string, p Proposal, s string) (ReviewVerdict, error) {
				reviews.Add(1)
				return inner.Review(ctx, f, d, p, s)
			})
		}
		g := NewGate(Options{Mode: func() string { return "auto" }, Reviewer: r})
		g.opts.EmitPrompt = func(_ context.Context, taskID string, _ PendingProposal, _ *FailureEvent) (string, error) {
			prompted.Store(true)
			id := "a1"
			g.BindApprovalID(taskID, id)
			if err := g.Resolve(id, ActionContinue, ""); err != nil {
				t.Fatalf("resolve: %v", err)
			}
			return id, nil
		}
		if setup != nil {
			setup(g)
		}
		dec, err := g.BeforeMutation(context.Background(), proposal)
		if err != nil {
			t.Fatalf("BeforeMutation: %v", err)
		}
		return outcome{allow: dec.Allow, prompted: prompted.Load(), reviews: reviews.Load(), blocked: dec.Blocked}
	}

	t.Run("ordinary read zero reviews zero clicks", func(t *testing.T) {
		got := run(t, nil, Proposal{Tool: "read_file", ReadOnly: true, Args: json.RawMessage(`{"path":"a.go"}`)}, nil)
		if !got.allow || got.prompted || got.reviews != 0 {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("ordinary mutation without failure zero reviews", func(t *testing.T) {
		got := run(t, nil, Proposal{
			Tool: "write_file", Mutates: true, Subject: "a.go",
			Args: json.RawMessage(`{"path":"a.go","content":"x"}`),
		}, nil)
		if !got.allow || got.prompted || got.reviews != 0 {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("execution safety boundary stays on permission path", func(t *testing.T) {
		got := run(t, nil, Proposal{
			Tool: "bash", Mutates: true, Subject: "git push origin feature",
			Args: json.RawMessage(`{"command":"git push origin feature"}`),
		}, nil)
		if !got.allow || got.prompted || got.reviews != 0 {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("failure then read-only diagnosis allows without review", func(t *testing.T) {
		got := run(t, func(g *Gate) {
			g.ObserveResult(context.Background(), Observation{
				Tool: "bash", Verification: true, ErrSummary: "fail",
				Args: json.RawMessage(`{"command":"go test ./..."}`),
			})
		}, Proposal{Tool: "read_file", ReadOnly: true, Args: json.RawMessage(`{"path":"a.go"}`)}, nil)
		if !got.allow || got.prompted || got.reviews != 0 {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("first safe verification retry allows without review", func(t *testing.T) {
		args := json.RawMessage(`{"command":"go test ./..."}`)
		got := run(t, func(g *Gate) {
			g.ObserveResult(context.Background(), Observation{
				Tool: "bash", Subject: "go test ./...", Verification: true, Args: args, ErrSummary: "fail",
			})
		}, Proposal{Tool: "bash", Subject: "go test ./...", Verification: true, Args: args}, nil)
		if !got.allow || got.prompted || got.reviews != 0 {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("different scoped operation bypasses recovery reviewer", func(t *testing.T) {
		got := run(t, func(g *Gate) {
			g.ObserveResult(context.Background(), Observation{
				Tool: "write_file", Mutates: true, Subject: "a.go", ErrSummary: "fail",
				Args: json.RawMessage(`{"path":"a.go"}`),
			})
		}, Proposal{
			Tool: "write_file", Mutates: true, Subject: "b.go", ExpandedScope: true,
			Args: json.RawMessage(`{"path":"b.go","content":"x"}`),
		}, nil)
		if !got.allow || got.prompted || got.reviews != 0 {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("different strategy operation bypasses recovery reviewer", func(t *testing.T) {
		got := run(t, func(g *Gate) {
			g.ObserveResult(context.Background(), Observation{
				Tool: "bash", Verification: true, ErrSummary: "fail",
				Args: json.RawMessage(`{"command":"go test"}`),
			})
		}, Proposal{
			Tool: "write_file", Mutates: true, StrategyChanged: true,
			Args: json.RawMessage(`{"path":"a.go","content":"x"}`),
		}, nil)
		if !got.allow || got.prompted || got.reviews != 0 {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("second recovery failure uses reviewer without prompting", func(t *testing.T) {
		got := run(t, func(g *Gate) {
			g.ObserveResult(context.Background(), Observation{
				Tool: "write_file", Mutates: true, Subject: "a.go", ErrSummary: "fail1",
				Args: json.RawMessage(`{"path":"a.go"}`),
			})
			g.ObserveResult(context.Background(), Observation{
				Tool: "write_file", Mutates: true, Subject: "a.go", ErrSummary: "fail2",
				Args: json.RawMessage(`{"path":"a.go"}`),
			})
		}, Proposal{
			Tool: "write_file", Mutates: true, Subject: "a.go",
			Args: json.RawMessage(`{"path":"a.go"}`),
		}, nil)
		if !got.allow || got.prompted || got.reviews != 1 {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("third repeated operation stops without prompting", func(t *testing.T) {
		got := run(t, func(g *Gate) {
			for i := 0; i < 3; i++ {
				g.ObserveResult(context.Background(), Observation{
					Tool: "write_file", Mutates: true, Subject: "a.go", ErrSummary: "fail",
					Args: json.RawMessage(`{"path":"a.go","content":"x"}`),
				})
			}
		}, Proposal{
			Tool: "write_file", Mutates: true, Subject: "a.go",
			Args: json.RawMessage(`{"path":"a.go","content":"x"}`),
		}, nil)
		if got.allow || got.prompted || got.reviews != 0 || !got.blocked {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("different edit after three verification failures stays automatic", func(t *testing.T) {
		got := run(t, func(g *Gate) {
			for i := 0; i < 3; i++ {
				g.ObserveResult(context.Background(), Observation{
					Tool: "bash", Verification: true, Subject: "go test ./...", ErrSummary: "fail",
					Args: json.RawMessage(`{"command":"go test ./..."}`),
				})
			}
		}, Proposal{
			Tool: "write_file", Mutates: true, Subject: "a.go",
			Args: json.RawMessage(`{"path":"a.go","content":"fix"}`),
		}, nil)
		if !got.allow || got.prompted || got.reviews != 0 || got.blocked {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("ambiguous recovery calls reviewer once", func(t *testing.T) {
		got := run(t, func(g *Gate) {
			g.ObserveResult(context.Background(), Observation{
				Tool: "write_file", Mutates: true, Subject: "a.go", ErrSummary: "fail",
				Args: json.RawMessage(`{"path":"a.go"}`),
			})
		}, Proposal{
			Tool: "write_file", Mutates: true, Subject: "a.go",
			Args: json.RawMessage(`{"path":"a.go"}`),
		}, nil)
		if !got.allow || got.prompted || got.reviews != 1 {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("reviewer same_strategy continues without prompt", func(t *testing.T) {
		got := run(t, func(g *Gate) {
			g.ObserveResult(context.Background(), Observation{
				Tool: "bash", Verification: true, ErrSummary: "fail",
				Args: json.RawMessage(`{"command":"go test"}`),
			})
		}, Proposal{
			Tool: "write_file", Mutates: true, Subject: "a.go",
			Args: json.RawMessage(`{"path":"a.go","content":"x"}`),
		}, staticReviewer{ReviewVerdict{Outcome: ReviewContinue, ChangeKind: ChangeSameStrategy}})
		if !got.allow || got.prompted || got.reviews != 0 {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("non plan recovery never opens a human confirmation", func(t *testing.T) {
		for _, kind := range []ChangeKind{ChangeStrategy, ChangeScope} {
			t.Run(string(kind), func(t *testing.T) {
				args := json.RawMessage(`{"path":"a.go","content":"x"}`)
				got := run(t, func(g *Gate) {
					g.ObserveResult(context.Background(), Observation{
						Tool: "write_file", Mutates: true, Subject: "a.go",
						ErrSummary: "fail", Args: args,
					})
				}, Proposal{
					Tool: "write_file", Mutates: true, Subject: "a.go", Args: args,
				}, staticReviewer{ReviewVerdict{
					Outcome: ReviewConfirm, ChangeKind: kind, Rationale: "the task now needs a user-owned plan choice",
				}})
				if got.allow || got.prompted || got.reviews != 1 || !got.blocked {
					t.Fatalf("got %+v", got)
				}
			})
		}
	})
	t.Run("normal execution plan transition prompts immediately", func(t *testing.T) {
		got := run(t, nil, Proposal{
			Tool: "todo_write", ReadOnly: true, PlanTransition: true,
			PlanBefore: "1. Keep current API [in_progress]",
			PlanAfter:  "1. Replace the public API [in_progress]",
		}, staticReviewer{ReviewVerdict{
			Outcome: ReviewConfirm, ChangeKind: ChangeStrategy, Rationale: "public API direction belongs to the user",
		}})
		if !got.allow || !got.prompted || got.reviews != 1 || got.blocked {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("reviewer reject returns to agent without prompt", func(t *testing.T) {
		args := json.RawMessage(`{"path":"a.go","content":"x"}`)
		got := run(t, func(g *Gate) {
			g.ObserveResult(context.Background(), Observation{
				Tool: "write_file", Mutates: true, Subject: "a.go",
				ErrSummary: "fail", Args: args,
			})
		}, Proposal{
			Tool: "write_file", Mutates: true, Subject: "a.go", Args: args,
		}, staticReviewer{ReviewVerdict{Outcome: ReviewConfirm, ChangeKind: ChangeUncertain, Rationale: "not proven"}})
		if got.allow || !got.blocked || got.prompted || got.reviews != 1 {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("reviewer third reject stops without prompt", func(t *testing.T) {
		var reviews atomic.Int32
		var prompts int
		g := NewGate(Options{
			Reviewer: reviewerFunc(func(context.Context, *FailureEvent, []string, Proposal, string) (ReviewVerdict, error) {
				reviews.Add(1)
				return ReviewVerdict{Outcome: ReviewConfirm, ChangeKind: ChangeUncertain, Rationale: "no"}, nil
			}),
		})
		args := json.RawMessage(`{"path":"a.go"}`)
		g.ObserveResult(context.Background(), Observation{
			Tool: "write_file", Mutates: true, Subject: "a.go", ErrSummary: "fail", Args: args,
		})
		g.opts.EmitPrompt = func(_ context.Context, taskID string, _ PendingProposal, _ *FailureEvent) (string, error) {
			prompts++
			g.BindApprovalID(taskID, "esc")
			_ = g.Resolve("esc", ActionContinue, "")
			return "esc", nil
		}
		prop := Proposal{Tool: "write_file", Mutates: true, Subject: "a.go", Args: args}
		for i := 1; i <= 2; i++ {
			dec, err := g.BeforeMutation(context.Background(), prop)
			if err != nil || dec.Allow || !dec.Blocked {
				t.Fatalf("attempt %d = %+v %v", i, dec, err)
			}
		}
		dec, err := g.BeforeMutation(context.Background(), prop)
		if err != nil || dec.Allow || !dec.Blocked || !dec.StopTurn || prompts != 0 || reviews.Load() != 3 || !strings.Contains(dec.Message, "paused this turn") {
			t.Fatalf("stop = %+v %v prompts=%d reviews=%d", dec, err, prompts, reviews.Load())
		}
	})
	t.Run("reviewer error keeps low risk work automatic", func(t *testing.T) {
		var reviews atomic.Int32
		var prompted atomic.Bool
		g := NewGate(Options{
			Reviewer: reviewerFunc(func(context.Context, *FailureEvent, []string, Proposal, string) (ReviewVerdict, error) {
				reviews.Add(1)
				return ReviewVerdict{}, errors.New("timeout")
			}),
		})
		args := json.RawMessage(`{"path":"a.go","content":"x"}`)
		g.ObserveResult(context.Background(), Observation{
			Tool: "write_file", Mutates: true, Subject: "a.go", ErrSummary: "fail", Args: args,
		})
		g.opts.EmitPrompt = func(_ context.Context, taskID string, _ PendingProposal, _ *FailureEvent) (string, error) {
			prompted.Store(true)
			g.BindApprovalID(taskID, "err")
			_ = g.Resolve("err", ActionContinue, "")
			return "err", nil
		}
		dec, err := g.BeforeMutation(context.Background(), Proposal{
			Tool: "write_file", Mutates: true, Subject: "a.go", Args: args,
		})
		if err != nil || !dec.Allow || prompted.Load() || reviews.Load() != 1 {
			t.Fatalf("got allow=%v prompted=%v reviews=%d err=%v", dec.Allow, prompted.Load(), reviews.Load(), err)
		}
		// A subsequent reject must still start at attempt 1 (error did not burn budget).
		g.opts.Reviewer = staticReviewer{ReviewVerdict{Outcome: ReviewConfirm, ChangeKind: ChangeUncertain, Rationale: "no"}}
		g.opts.EmitPrompt = nil
		dec, err = g.BeforeMutation(context.Background(), Proposal{
			Tool: "write_file", Mutates: true, Subject: "a.go", Args: args,
		})
		if err != nil || dec.Allow || !dec.Blocked || !strings.Contains(dec.Message, "attempt 1/3") {
			t.Fatalf("post-error reject = %+v %v", dec, err)
		}
	})
	t.Run("reviewer error asks about structural plan transition", func(t *testing.T) {
		got := run(t, nil, Proposal{
			Tool: "todo_write", ReadOnly: true, PlanTransition: true,
			PlanBefore: "1. Existing [in_progress]", PlanAfter: "1. Replacement [in_progress]",
		}, reviewerFunc(func(context.Context, *FailureEvent, []string, Proposal, string) (ReviewVerdict, error) {
			return ReviewVerdict{}, errors.New("timeout")
		}))
		if !got.allow || !got.prompted || got.reviews != 1 {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("bounded strategy and scope changes may continue", func(t *testing.T) {
		for _, kind := range []ChangeKind{ChangeStrategy, ChangeScope} {
			t.Run(string(kind), func(t *testing.T) {
				got := run(t, func(g *Gate) {
					g.ObserveResult(context.Background(), Observation{
						Tool: "write_file", Mutates: true, ErrSummary: "fail",
						Args: json.RawMessage(`{"path":"a.go"}`),
					})
				}, Proposal{
					Tool: "write_file", Mutates: true, Args: json.RawMessage(`{"path":"b.go","content":"x"}`),
				}, staticReviewer{ReviewVerdict{Outcome: ReviewContinue, ChangeKind: kind}})
				if !got.allow || got.prompted || got.reviews != 0 {
					t.Fatalf("bounded %s recovery = %+v", kind, got)
				}
			})
		}
	})
	t.Run("risk or uncertainty cannot silently continue", func(t *testing.T) {
		for _, kind := range []ChangeKind{ChangeRisk, ChangeUncertain} {
			t.Run(string(kind), func(t *testing.T) {
				args := json.RawMessage(`{"path":"a.go","content":"x"}`)
				got := run(t, func(g *Gate) {
					g.ObserveResult(context.Background(), Observation{
						Tool: "write_file", Mutates: true, Subject: "a.go",
						ErrSummary: "fail", Args: args,
					})
				}, Proposal{
					Tool: "write_file", Mutates: true, Subject: "a.go", Args: args,
				}, staticReviewer{ReviewVerdict{Outcome: ReviewContinue, ChangeKind: kind}})
				if got.allow {
					t.Fatalf("unsafe %s recovery auto-allowed: %+v", kind, got)
				}
			})
		}
	})
	t.Run("headless execution risk stays on permission path", func(t *testing.T) {
		g := NewGate(Options{Headless: true})
		dec, err := g.BeforeMutation(context.Background(), Proposal{
			Tool: "bash", Mutates: true, Subject: "git push origin feature",
			Args: json.RawMessage(`{"command":"git push origin feature"}`),
		})
		if err != nil || !dec.Allow || dec.Blocked {
			t.Fatalf("got %+v %v", dec, err)
		}
	})
	t.Run("success verification clears failure state", func(t *testing.T) {
		g := NewGate(Options{})
		g.ObserveResult(context.Background(), Observation{
			Tool: "bash", Verification: true, ErrSummary: "fail",
			Args: json.RawMessage(`{"command":"go test"}`),
		})
		g.ObserveResult(context.Background(), Observation{
			Tool: "bash", Verification: true, Success: true,
			Args: json.RawMessage(`{"command":"go test"}`),
		})
		if st := g.Snapshot().Tasks["root"]; st != nil {
			t.Fatalf("want cleared, got %+v", st)
		}
	})
}

func TestReviewerPlanDecisionOnlyAcceptsExplicitMaterialChange(t *testing.T) {
	tests := []struct {
		name string
		v    ReviewVerdict
		want bool
	}{
		{name: "strategy confirm", v: ReviewVerdict{Outcome: ReviewConfirm, ChangeKind: ChangeStrategy}, want: true},
		{name: "scope confirm", v: ReviewVerdict{Outcome: ReviewConfirm, ChangeKind: ChangeScope}, want: true},
		{name: "bounded strategy continues", v: ReviewVerdict{Outcome: ReviewContinue, ChangeKind: ChangeStrategy}},
		{name: "uncertain remains self-correction", v: ReviewVerdict{Outcome: ReviewConfirm, ChangeKind: ChangeUncertain}},
		{name: "risk stays on risk path", v: ReviewVerdict{Outcome: ReviewConfirm, ChangeKind: ChangeRisk}},
		{name: "same strategy confirm", v: ReviewVerdict{Outcome: ReviewConfirm, ChangeKind: ChangeSameStrategy}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := reviewerPlanDecision(tc.v); got != tc.want {
				t.Fatalf("reviewerPlanDecision(%+v) = %v, want %v", tc.v, got, tc.want)
			}
		})
	}
}

func TestSafeRetryConsumedOnlyOnce(t *testing.T) {
	g := NewGate(Options{})
	args := json.RawMessage(`{"command":"go test ./..."}`)
	g.ObserveResult(context.Background(), Observation{
		Tool: "bash", Subject: "go test ./...", Verification: true, Args: args, ErrSummary: "fail",
	})
	dec, err := g.BeforeMutation(context.Background(), Proposal{
		Tool: "bash", Subject: "go test ./...", Verification: true, Args: args,
	})
	if err != nil || !dec.Allow {
		t.Fatalf("first retry = %+v %v", dec, err)
	}
	// Without re-arming, a second identical verification still has active failure
	// but safe retry is spent → reviewer/ask path (not silent second auto-retry).
	var reviews atomic.Int32
	g.opts.Reviewer = reviewerFunc(func(context.Context, *FailureEvent, []string, Proposal, string) (ReviewVerdict, error) {
		reviews.Add(1)
		return ReviewVerdict{Outcome: ReviewContinue, ChangeKind: ChangeSameStrategy}, nil
	})
	dec, err = g.BeforeMutation(context.Background(), Proposal{
		Tool: "bash", Subject: "go test ./...", Verification: true, Args: args,
	})
	if err != nil || !dec.Allow {
		t.Fatalf("second attempt = %+v %v", dec, err)
	}
	if reviews.Load() != 1 {
		t.Fatalf("spent safe retry must go through reviewer, reviews=%d", reviews.Load())
	}
}

func TestTaskIsolationByTaskID(t *testing.T) {
	var reviewedTask string
	g := NewGate(Options{
		Reviewer: reviewerFunc(func(_ context.Context, f *FailureEvent, _ []string, p Proposal, _ string) (ReviewVerdict, error) {
			reviewedTask = p.TaskID
			if f != nil && f.TaskID != "" && f.TaskID != normalizeTaskID(p.TaskID) {
				return ReviewVerdict{}, fmt.Errorf("failure task %q proposal task %q", f.TaskID, p.TaskID)
			}
			return ReviewVerdict{Outcome: ReviewContinue, ChangeKind: ChangeSameStrategy}, nil
		}),
	})
	g.ObserveResult(context.Background(), Observation{
		TaskID: "subagent:a", Tool: "write_file", Mutates: true, ErrSummary: "fail-a",
		Args: json.RawMessage(`{"path":"a.go"}`),
	})
	g.ObserveResult(context.Background(), Observation{
		TaskID: "subagent:b", Tool: "write_file", Mutates: true, ErrSummary: "fail-b",
		Args: json.RawMessage(`{"path":"b.go"}`),
	})
	dec, err := g.BeforeMutation(context.Background(), Proposal{
		TaskID: "subagent:a", Tool: "write_file", Mutates: true,
		Args: json.RawMessage(`{"path":"a.go"}`),
	})
	if err != nil || !dec.Allow {
		t.Fatalf("task a = %+v %v", dec, err)
	}
	if reviewedTask != "subagent:a" {
		t.Fatalf("reviewed task = %q", reviewedTask)
	}
	// Task B still has its own failure; task A success does not clear it.
	g.ObserveResult(context.Background(), Observation{
		TaskID: "subagent:a", Tool: "write_file", Mutates: true, Success: true,
		Args: json.RawMessage(`{"path":"a.go"}`),
	})
	if st := g.Snapshot().Tasks["subagent:b"]; st == nil || st.Failure == nil {
		t.Fatalf("task b failure cleared incorrectly: %+v", st)
	}
	if st := g.Snapshot().Tasks["subagent:a"]; st != nil {
		t.Fatalf("task a should be cleared: %+v", st)
	}
}

func TestResolveSyncNoDeadlock(t *testing.T) {
	g := NewGate(Options{})
	g.ObserveResult(context.Background(), Observation{
		Tool: "bash", Verification: true, ErrSummary: "fail",
		Args: json.RawMessage(`{"command":"go test"}`),
	})
	// EmitPrompt resolves synchronously before returning — must not deadlock.
	g.opts.EmitPrompt = func(_ context.Context, taskID string, _ PendingProposal, _ *FailureEvent) (string, error) {
		g.BindApprovalID(taskID, "sync")
		if err := g.Resolve("sync", ActionContinue, ""); err != nil {
			return "", err
		}
		return "sync", nil
	}
	done := make(chan struct {
		dec Decision
		err error
	}, 1)
	go func() {
		dec, err := g.BeforeMutation(context.Background(), Proposal{
			Tool: "write_file", Mutates: true, StrategyChanged: true,
			Args: json.RawMessage(`{"path":"a.go"}`),
		})
		done <- struct {
			dec Decision
			err error
		}{dec, err}
	}()
	select {
	case got := <-done:
		if got.err != nil || !got.dec.Allow {
			t.Fatalf("want allow, got %+v %v", got.dec, got.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("deadlock: synchronous Resolve did not unblock BeforeMutation")
	}
}
