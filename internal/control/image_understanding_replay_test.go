package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"reasonix/internal/attachment"
	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func replayFixtureMessages(role provider.Role) []provider.Message {
	image := provider.Message{ID: "image", Role: role, Content: "original text", ImageInputs: []attachment.ImageInput{{Kind: attachment.KindURL, URL: "https://fixture.invalid/image.png"}}}
	text := provider.Message{ID: "text", Role: provider.RoleUser, Content: "plain follow-up"}
	if role == provider.RoleTool {
		return []provider.Message{text, image}
	}
	return []provider.Message{image, text}
}

func TestHistoricalImageCanRecoverWithoutRewritingOrNegativeCaching(t *testing.T) {
	broken := true
	c := newOwnedTestController(t, Options{WorkspaceRoot: t.TempDir(), ImageRouteConfig: config.Default(), VisionModel: "fixture/vision",
		VisionProviderResolver: func(string) (provider.Provider, error) {
			if broken {
				return nil, errors.New("image service unavailable")
			}
			return visionSummaryTestProvider{}, nil
		},
	})
	messages := replayFixtureMessages(provider.RoleUser)
	before, _ := json.Marshal(messages)
	first, err := c.ResolveRequestImagesForModel(t.Context(), messages, "fixture/main", false)
	if err != nil || !strings.Contains(first[0].Content, "unavailable") {
		t.Fatalf("degraded request: %v", err)
	}
	broken = false
	recovered, err := c.ResolveRequestImagesForModel(t.Context(), messages, "fixture/main", false)
	if err != nil || !strings.Contains(recovered[0].Content, "Revenue 42") || strings.Contains(recovered[0].Content, "unavailable") {
		t.Fatalf("repaired image stayed unavailable: %+v %v", recovered, err)
	}
	after, _ := json.Marshal(messages)
	if !bytes.Equal(before, after) {
		t.Fatal("canonical image history mutated")
	}
}

type replayFailureProvider struct{ err error }

func (replayFailureProvider) Name() string { return "vision-fixture" }
func (replayFailureProvider) ModelInfo() provider.ModelInfo {
	return provider.ModelInfo{InputModalities: []provider.ModelModality{provider.ModalityImage}}
}
func (p replayFailureProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 1)
	if p.err != nil {
		ch <- provider.Chunk{Type: provider.ChunkError, Err: p.err}
	}
	close(ch)
	return ch, nil
}

func TestImageUnderstandingFailureDoesNotPoisonTextOrToolResults(t *testing.T) {
	for _, role := range []provider.Role{provider.RoleUser, provider.RoleTool} {
		for _, failure := range []string{"resolution", "capability", "upstream", "timeout", "empty"} {
			t.Run(string(role)+"/"+failure, func(t *testing.T) {
				var notices []event.Event
				c := newOwnedTestController(t, Options{WorkspaceRoot: t.TempDir(), ImageRouteConfig: config.Default(), VisionModel: "fixture/vision",
					Sink: event.FuncSink(func(e event.Event) {
						if e.Code == event.NoticeCodeImageReplayUnavailable {
							notices = append(notices, e)
						}
					}),
					VisionProviderResolver: func(string) (provider.Provider, error) {
						switch failure {
						case "resolution":
							return nil, errors.New("unknown vision model fixture/vision")
						case "capability":
							return &reviewImageProvider{textOnly: true}, nil
						case "upstream":
							return replayFailureProvider{err: &provider.APIError{Status: 503, Body: `{"error":{"message":"image service unavailable"}}`}}, nil
						case "empty":
							return replayFailureProvider{}, nil
						default:
							return replayFailureProvider{err: context.DeadlineExceeded}, nil
						}
					}})
				messages := replayFixtureMessages(role)
				before, _ := json.Marshal(messages)
				var first []byte
				for attempt := range 2 {
					got, err := c.ResolveRequestImagesForModel(t.Context(), messages, "fixture/main", false)
					if err != nil {
						t.Fatal(err)
					}
					i := 0
					if role == provider.RoleTool {
						i = 1
					}
					if len(got[i].ImageInputs) != 0 || len(got[i].Images) != 0 || !strings.Contains(got[i].Content, "original text") || !strings.Contains(got[i].Content, "not") {
						t.Fatalf("bad fallback: %+v", got)
					}
					if role == provider.RoleTool && !strings.Contains(got[i].Content, "already executed") {
						t.Fatal("lost tool execution evidence")
					}
					wire, _ := json.Marshal(provider.ModelMessages(got))
					if attempt == 0 {
						first = wire
					} else if !bytes.Equal(first, wire) {
						t.Fatal("repeated failure changed provider-visible prefix")
					}
				}
				after, _ := json.Marshal(messages)
				if !bytes.Equal(before, after) {
					t.Fatal("rewrote canonical history")
				}
				if len(notices) != 2 || notices[0].Detail == "" || notices[0].Diagnostic != nil {
					t.Fatalf("missing visible diagnosis: %+v", notices)
				}
				current := []provider.Message{messages[0]}
				if role == provider.RoleTool {
					current = []provider.Message{messages[1]}
					current[0].Role = provider.RoleUser
				}
				if _, err := c.ResolveRequestImagesForModel(t.Context(), current, "fixture/main", false); err == nil || provider.DiagnoseFailure(err).Kind != provider.FailureKindImageInput {
					t.Fatalf("current user image silently omitted: %v", err)
				}
			})
		}
	}
}

func TestImageReplayPreservesCancellationAndHealthySiblings(t *testing.T) {
	for _, cancelTurn := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		c := newOwnedTestController(t, Options{WorkspaceRoot: t.TempDir(), ImageRouteConfig: config.Default(), VisionModel: "fixture/vision",
			VisionProviderResolver: func(string) (provider.Provider, error) {
				if cancelTurn {
					cancel()
					return nil, errors.New("concurrent failure")
				}
				return nil, context.Canceled
			}})
		_, err := c.ResolveRequestImagesForModel(ctx, replayFixtureMessages(provider.RoleUser), "fixture/main", false)
		cancel()
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation swallowed: %v", err)
		}
	}
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	cancelled := newOwnedTestController(t, Options{WorkspaceRoot: t.TempDir(), ImageRouteConfig: config.Default(), VisionModel: "fixture/vision"})
	if _, err := cancelled.ResolveRequestImagesForModel(ctx, replayFixtureMessages(provider.RoleUser), "fixture/main", false); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("parent deadline lost: %v", err)
	}
	calls := 0
	c := newOwnedTestController(t, Options{WorkspaceRoot: t.TempDir(), ImageRouteConfig: config.Default(), VisionModel: "fixture/vision",
		VisionProviderResolver: func(string) (provider.Provider, error) {
			calls++
			if calls == 1 {
				return nil, errors.New("bad earlier image")
			}
			return visionSummaryTestProvider{}, nil
		}})
	ms := replayFixtureMessages(provider.RoleUser)
	ms = append(ms[:1], append([]provider.Message{ms[0]}, ms[1:]...)...)
	got, err := c.ResolveRequestImagesForModel(t.Context(), ms, "fixture/main", false)
	if err != nil || !strings.Contains(got[1].Content, "Revenue 42") || !strings.Contains(got[0].Content, "unavailable") {
		t.Fatalf("healthy sibling lost: %+v %v", got, err)
	}
}
