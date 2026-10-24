package control

import (
	"context"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/runtime/isolation"
)

type commandResults struct {
	IsolationResultsAPI
	discard   IsolatedDiscardRequest
	recovered isolation.RecoveryRequest
	refused   error
	catalog   *isolation.Catalog
}

func TestIsolationCommandShowsDescendantDirectTarget(t *testing.T) {
	api := &commandResults{catalog: &isolation.Catalog{Enabled: true, Results: []isolation.ResultSummary{{
		ID: "iso_12345678", ParentID: "iso_87654321", TargetWorkspace: "/direct-parent", State: isolation.Pending,
	}}}}
	text, err := IsolationCommandText(t.Context(), api, "list")
	if err != nil || !strings.Contains(text, "parent=iso_87654321 target=/direct-parent") {
		t.Fatalf("descendant lost its direct integration target: %q %v", text, err)
	}
}

func TestIsolationCommandPreviewsAndConfirmsRegisteredDescendantRecovery(t *testing.T) {
	target := &isolation.RecoveryTarget{ResultID: "iso_child", Workspace: "/direct-parent", Runtime: "current", Revision: "journal", Paths: []string{"/direct-parent/file"}}
	api := &commandResults{catalog: &isolation.Catalog{Enabled: true, Results: []isolation.ResultSummary{{ID: "int_child", ResultID: "iso_child", Recovery: target}}}}
	text, err := IsolationCommandText(t.Context(), api, "recover int_child")
	if err != nil || !strings.Contains(text, "/direct-parent/file") || api.recovered.Runtime != "" {
		t.Fatalf("descendant preview: %q %v", text, err)
	}
	command := strings.TrimPrefix(text[strings.LastIndex(text, "\n")+1:], "/isolated ")
	if _, err := IsolationCommandText(t.Context(), api, command); err != nil {
		t.Fatal(err)
	}
	if api.recovered != target.Request() {
		t.Fatalf("confirmation lost descendant identity: %+v", api.recovered)
	}
	api.recovered = isolation.RecoveryRequest{}
	if _, err := IsolationCommandText(t.Context(), api, "recover iso_unregistered"); err == nil || api.recovered.Runtime != "" {
		t.Fatal("unregistered descendant accepted")
	}
}

func (f *commandResults) IsolationResults(context.Context) (isolation.Catalog, error) {
	if f.catalog != nil {
		return *f.catalog, nil
	}
	return isolation.Catalog{Enabled: true, RecoveryRequired: true, Recovery: &isolation.RecoveryTarget{Workspace: "/fixture", Runtime: "runtime-a", Revision: strings.Repeat("b", 64), Paths: []string{"/fixture/file"}}}, nil
}

func TestRecoveryConfirmationSizeIsIndependentOfPreviewPathCount(t *testing.T) {
	fixture := &commandResults{}
	catalog, _ := fixture.IsolationResults(t.Context())
	for range 5_000 {
		catalog.Recovery.Paths = append(catalog.Recovery.Paths, "/fixture/"+strings.Repeat("long-path/", 40)+"file")
	}
	fixture.catalog = &catalog
	text, err := IsolationCommandText(t.Context(), fixture, "recover")
	if err != nil {
		t.Fatal(err)
	}
	command := strings.TrimPrefix(text[strings.LastIndex(text, "\n")+1:], "/isolated ")
	if len(command) > 4096 {
		t.Fatalf("confirmation contains preview paths: %d bytes", len(command))
	}
	if _, err := IsolationCommandText(t.Context(), fixture, command); err != nil {
		t.Fatal(err)
	}
	if fixture.recovered != catalog.Recovery.Request() {
		t.Fatalf("confirmation changed binding: %+v", fixture.recovered)
	}
}
func (*commandResults) PreviewIsolatedDiscard(_ context.Context, id string) (isolation.DiscardPreview, error) {
	return isolation.DiscardPreview{ID: id, Revision: strings.Repeat("a", 64), CandidateRoot: "/candidate", Entries: 7, State: isolation.Pending}, nil
}
func (f *commandResults) DiscardIsolated(_ context.Context, request IsolatedDiscardRequest) error {
	f.discard = request
	return f.refused
}
func (f *commandResults) RecoverIsolatedIntegration(_ context.Context, target isolation.RecoveryRequest) error {
	f.recovered = target
	return f.refused
}

func TestIsolationCommandsRequireAndPreserveExplicitPreviewConfirmations(t *testing.T) {
	fixture := &commandResults{}
	text, err := IsolationCommandText(t.Context(), fixture, "discard iso_12345678")
	if err != nil || fixture.discard.ID != "" || !strings.Contains(text, "/candidate") || !strings.Contains(text, "ignored files") {
		t.Fatalf("preview: %s %v", text, err)
	}
	command := strings.TrimPrefix(text[strings.LastIndex(text, "\n")+1:], "/isolated ")
	fixture.refused = errors.New("candidate changed")
	if _, err := IsolationCommandText(t.Context(), fixture, command); !errors.Is(err, fixture.refused) {
		t.Fatal(err)
	}
	if fixture.discard.ID != "iso_12345678" || fixture.discard.Revision != strings.Repeat("a", 64) {
		t.Fatalf("confirmation altered identity: %+v", fixture.discard)
	}
	fixture.refused = nil
	text, err = IsolationCommandText(t.Context(), fixture, "recover")
	if err != nil || fixture.recovered.Runtime != "" || !strings.Contains(text, "/fixture/file") {
		t.Fatalf("recovery preview: %s %v", text, err)
	}
	command = strings.TrimPrefix(text[strings.LastIndex(text, "\n")+1:], "/isolated ")
	if _, err := IsolationCommandText(t.Context(), fixture, command); err != nil {
		t.Fatal(err)
	}
	if fixture.recovered.Runtime != "runtime-a" || fixture.recovered.Revision != strings.Repeat("b", 64) {
		t.Fatalf("recovery token altered: %+v", fixture.recovered)
	}
}
