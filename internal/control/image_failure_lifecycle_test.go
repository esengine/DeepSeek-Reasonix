package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/session"
	"reasonix/internal/tool"
	"reasonix/internal/transcript"
)

func TestImageFailureSurvivesCleanupAndColdExport(t *testing.T) {
	for _, historical := range []bool{false, true} {
		t.Run(map[bool]string{true: "historical-text-continues", false: "current-image-fails"}[historical], func(t *testing.T) {
			service, err := session.NewService("local", session.NewFilesystemPersistence(t.TempDir()))
			if err != nil {
				t.Fatal(err)
			}
			runtime, err := service.Create(t.Context(), session.CreateOptions{SessionID: "image-failure"})
			if err != nil {
				t.Fatal(err)
			}
			p := &reviewImageProvider{requests: make(chan provider.Request, 8), textOnly: true}
			exec := agent.New(p, tool.NewRegistry(), agent.NewSession("stable system"), agent.Options{ModelRef: "fixture/main"}, event.Discard)
			c := newOwnedTestController(t, Options{WorkspaceRoot: t.TempDir(), Runner: exec, Executor: exec, ModelRef: "fixture/main",
				SessionService: service, SessionRuntime: runtime, ExclusiveSession: true,
				ImageRouteConfig: config.Default(), VisionModel: "fixture/vision",
				VisionProviderResolver: func(string) (provider.Provider, error) {
					return nil, errors.New("unknown vision model fixture/vision API_KEY=sk-fixture-secret-123456")
				},
			})
			image := replayFixtureMessages(provider.RoleUser)[0]
			ctx := t.Context()
			if historical {
				if err := c.RecordSessionMessages(ctx, "historical-image", []provider.Message{image}); err != nil {
					t.Fatal(err)
				}
				exec.Session().Add(image)
			} else {
				ctx = agent.WithUserImageInputs(ctx, image.ImageInputs)
			}
			err = c.RunTurn(ctx, "plain follow-up")
			if historical {
				if err != nil || len(p.requests) != 1 {
					t.Fatalf("text did not reach main provider: %v requests=%d", err, len(p.requests))
				}
				req := <-p.requests
				foundHistory := false
				for _, m := range req.Messages {
					foundHistory = foundHistory || strings.Contains(m.Content, "original text") && strings.Contains(m.Content, "unavailable")
					if len(m.ImageInputs)+len(m.Images) != 0 {
						t.Fatal("broken image reached text model")
					}
				}
				if !foundHistory {
					t.Fatal("request did not exercise historical image replay")
				}
			} else {
				if err == nil || len(p.requests) != 0 {
					t.Fatalf("current attachment was silently omitted: %v", err)
				}
				rows := transcript.History(c.sessionEventStore().Snapshot().Projection.Messages, transcript.HistoryOptions{})
				found := false
				for _, row := range rows {
					if row.Code == event.NoticeCodeProviderRequestFailed {
						found = strings.Contains(row.Detail, "unknown vision model fixture/vision") && row.Diagnostic.Kind == provider.FailureKindImageInput
					}
				}
				if !found {
					t.Fatalf("cleanup lost original detail: %+v", rows)
				}
			}
			ref := runtime.Ref()
			if !historical {
				for _, commit := range terminationCommitHistory(t, c) {
					for _, e := range commit.Events {
						if e.Kind == "diagnostic" && strings.Contains(string(e.Payload), event.NoticeCodeProviderRequestFailed) {
							t.Fatal("canonical failure duplicated as display notice")
						}
					}
				}
			}
			c.Close()
			<-c.closeFinalized
			if err := service.CloseAll(context.Background()); err != nil {
				t.Fatal(err)
			}
			snapshot, err := service.Query().CaptureDiagnosticSnapshot(t.Context(), ref)
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			if err := WriteColdSessionDiagnostics(t.Context(), &out, service.Query(), snapshot, GoalDiagnosticMetadata{}, nil); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), "unknown vision model fixture/vision") || strings.Contains(out.String(), "sk-fixture-secret-123456") {
				t.Fatalf("cold export lost/redaction failed: %s", out.String())
			}
			if historical && !strings.Contains(out.String(), event.NoticeCodeImageReplayUnavailable) {
				t.Fatal("recovered image warning not persisted")
			}
			assertColdImageFailureDetails(t, service, ref)
		})
	}
}

func TestDesktopImagePrepassFailureIsDurableBeforeAgentRun(t *testing.T) {
	service, err := session.NewService("local", session.NewFilesystemPersistence(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.CloseAll(context.Background()) })
	runtime, err := service.Create(t.Context(), session.CreateOptions{SessionID: "desktop-image-prepass"})
	if err != nil {
		t.Fatal(err)
	}
	p := &reviewImageProvider{requests: make(chan provider.Request, 1), textOnly: true}
	exec := agent.New(p, tool.NewRegistry(), agent.NewSession("system"), agent.Options{ModelRef: "fixture/main"}, event.Discard)
	done := make(chan event.Event, 1)
	native := false
	c := newOwnedTestController(t, Options{WorkspaceRoot: t.TempDir(), Runner: exec, Executor: exec, ModelRef: "fixture/main", FrozenImageInput: &native,
		SessionService: service, SessionRuntime: runtime, ExclusiveSession: true, ImageRouteConfig: config.Default(), VisionModel: "fixture/vision",
		VisionProviderResolver: func(string) (provider.Provider, error) { return nil, errors.New("unknown vision model fixture/vision") },
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone {
				done <- e
			}
		}),
	})
	draft, err := c.StageImage(t.Context(), "image.png", "image/png", "data:image/png;base64,"+tinyPNG)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.SubmitIdentified(SubmissionRequest{Input: "inspect current image", Attachments: []SubmissionAttachment{{ClientAttachmentID: "image", DraftID: draft.ID}}}); err != nil {
		t.Fatal(err)
	}
	terminal := waitTurnDoneEvent(t, done)
	waitIdleAdmission(t, c)
	if terminal.Err == nil || len(p.requests) != 0 {
		t.Fatal("prepass did not stop main provider")
	}
	count := 0
	for _, commit := range terminationCommitHistory(t, c) {
		for _, e := range commit.Events {
			if e.Kind != "diagnostic" {
				continue
			}
			var body struct {
				Record transcript.Message `json:"displayRecord"`
			}
			if err := json.Unmarshal(e.Payload, &body); err != nil {
				t.Fatal(err)
			}
			if body.Record.Code != event.NoticeCodeProviderRequestFailed {
				continue
			}
			count++
			if !strings.Contains(body.Record.Detail, "unknown vision model fixture/vision") || body.Record.Diagnostic.Kind != provider.FailureKindImageInput || commit.Events[len(commit.Events)-1].Kind != "turn/end" {
				t.Fatalf("prepass detail not atomic: %+v", body.Record)
			}
		}
	}
	if count != 1 {
		t.Fatalf("persisted prepass notices=%d", count)
	}
	c.Close()
	<-c.closeFinalized
	if err := service.CloseAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertColdImageFailureDetails(t, service, runtime.Ref())
}

func assertColdImageFailureDetails(t *testing.T, service *session.Service, ref session.SessionRef) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		page, err := service.Query().ReadHistoryWindow(t.Context(), ref, session.HistoryWindowRequest{Anchor: "newest"})
		if err != nil {
			t.Fatal(err)
		}
		if page.Status == "preparing" && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
			continue
		}
		if page.Status != "ready" {
			t.Fatalf("cold history status=%s", page.Status)
		}
		count := 0
		for _, m := range page.Messages {
			body := m.Inline
			if m.ContentRef != nil {
				body, err = service.Query().ReadContent(t.Context(), ref, *m.ContentRef, 0, m.ContentRef.Bytes)
				if err != nil {
					t.Fatal(err)
				}
			}
			if bytes.Contains(body, []byte("unknown vision model fixture/vision")) {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("cold history causes=%d, want one visible failure", count)
		}
		return
	}
}
