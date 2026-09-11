package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"reasonix/internal/billing"
	"reasonix/internal/control"
)

type billingRuntimeController struct {
	stubSessionAPI
	status  control.RuntimeStatus
	started chan struct{}
	release chan struct{}
	balance *billing.Balance
	err     error
}

func (c *billingRuntimeController) RuntimeStatus() control.RuntimeStatus { return c.status }
func (c *billingRuntimeController) SessionPath() string                  { return "" }

func (c *billingRuntimeController) Balance(ctx context.Context) (*billing.Balance, error) {
	if c.started != nil {
		close(c.started)
	}
	if c.release != nil {
		select {
		case <-c.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return c.balance, c.err
}

func TestSetDesktopCurrencyRebindsLedgerWithoutControllerRebuild(t *testing.T) {
	isolateDesktopUserDirs(t)
	ctrl := &billingRuntimeController{status: control.RuntimeStatus{Running: true}}
	ledger := billing.NewLedger()
	ledger.Add(billing.CostQuote{
		Original: billing.Money{Amount: "1", Currency: "USD"},
		Valuations: map[string]billing.Valuation{
			"USD": {Money: billing.Money{Amount: "1", Currency: "USD"}, Basis: billing.BasisIdentity},
			"CNY": {Money: billing.Money{Amount: "7", Currency: "CNY"}, Basis: billing.BasisOfficialTable},
		},
		Selected:        &billing.Money{Amount: "1", Currency: "USD"},
		Estimated:       true,
		CostComplete:    true,
		DisplayComplete: true,
		Complete:        true,
		DisplayStatus:   billing.DisplayStatusMatched,
		ModelRef:        "deepseek-flash/deepseek-v4-flash",
	}, billing.UsageTokens{PromptTokens: 1_000_000}, nowUTC())
	tab := &WorkspaceTab{
		ID: "billing", Ctrl: ctrl,
		usageTelemetry: sessionUsageStats{CostLedger: ledger, SessionCost: 1, SessionCurrency: "USD", SessionCostComplete: true},
	}
	app := &App{tabs: map[string]*WorkspaceTab{tab.ID: tab}, tabOrder: []string{tab.ID}, activeTabID: tab.ID}

	if err := app.SetDesktopCurrency("CNY"); err != nil {
		t.Fatalf("SetDesktopCurrency during active work: %v", err)
	}
	if tab.Ctrl != ctrl {
		t.Fatal("display-only currency change replaced the controller")
	}
	quote := tab.telemetrySnapshot().Usage.SessionCostQuote
	if quote == nil || quote.Selected == nil || quote.Selected.Currency != "CNY" || quote.Selected.Amount != "7" {
		t.Fatalf("rebound quote = %+v, want selected CNY 7", quote)
	}
}

func TestBalanceForTabRejectsPreviousSessionResponse(t *testing.T) {
	isolateDesktopUserDirs(t)
	ctrl := &billingRuntimeController{
		started: make(chan struct{}),
		release: make(chan struct{}),
		balance: &billing.Balance{Available: true, Infos: []billing.Info{{Currency: "CNY", TotalBalance: "70.16"}}},
	}
	tab := &WorkspaceTab{ID: "billing", Ctrl: ctrl}
	tab.resetTelemetry("old-session.jsonl")
	app := &App{
		ctx: context.Background(), tabs: map[string]*WorkspaceTab{tab.ID: tab},
		tabOrder: []string{tab.ID}, activeTabID: tab.ID,
	}

	done := make(chan BalanceInfo, 1)
	go func() { done <- app.BalanceForTab(tab.ID) }()
	<-ctrl.started
	tab.resetTelemetry("new-session.jsonl")
	close(ctrl.release)
	<-done

	tab.telemMu.Lock()
	display := tab.runtimeCostDisplayCurrency
	tab.telemMu.Unlock()
	if display != "" {
		t.Fatalf("previous-session balance rebound new session to %q", display)
	}
}

func TestBalanceForTabRejectsPreviousControllerResponse(t *testing.T) {
	isolateDesktopUserDirs(t)
	ctrl := &billingRuntimeController{
		started: make(chan struct{}), release: make(chan struct{}),
		balance: &billing.Balance{Available: true, Infos: []billing.Info{{Currency: "CNY", TotalBalance: "70.16"}}},
	}
	tab := &WorkspaceTab{ID: "billing", Ctrl: ctrl}
	app := &App{ctx: context.Background(), tabs: map[string]*WorkspaceTab{tab.ID: tab}, tabOrder: []string{tab.ID}, activeTabID: tab.ID}

	done := make(chan BalanceInfo, 1)
	go func() { done <- app.BalanceForTab(tab.ID) }()
	<-ctrl.started
	app.mu.Lock()
	tab.Ctrl = &billingRuntimeController{}
	app.mu.Unlock()
	close(ctrl.release)
	<-done

	tab.telemMu.Lock()
	display := tab.runtimeCostDisplayCurrency
	tab.telemMu.Unlock()
	if display != "" {
		t.Fatalf("previous-controller balance rebound current controller to %q", display)
	}
}

func TestBalanceForTabRejectsResponseAfterExplicitCurrencyChange(t *testing.T) {
	isolateDesktopUserDirs(t)
	ctrl := &billingRuntimeController{
		started: make(chan struct{}), release: make(chan struct{}),
		balance: &billing.Balance{Available: true, Infos: []billing.Info{{Currency: "CNY", TotalBalance: "70.16"}}},
	}
	tab := &WorkspaceTab{ID: "billing", Ctrl: ctrl}
	app := &App{ctx: context.Background(), tabs: map[string]*WorkspaceTab{tab.ID: tab}, tabOrder: []string{tab.ID}, activeTabID: tab.ID}

	done := make(chan BalanceInfo, 1)
	go func() { done <- app.BalanceForTab(tab.ID) }()
	<-ctrl.started
	if err := app.SetDesktopCurrency("USD"); err != nil {
		t.Fatalf("SetDesktopCurrency: %v", err)
	}
	close(ctrl.release)
	<-done

	tab.telemMu.Lock()
	display := tab.runtimeCostDisplayCurrency
	tab.telemMu.Unlock()
	if display != "" {
		t.Fatalf("auto balance response overrode explicit USD with %q", display)
	}
}

func TestBalanceForTabFailureDoesNotChangeDisplayBinding(t *testing.T) {
	isolateDesktopUserDirs(t)
	ctrl := &billingRuntimeController{err: errors.New("balance unavailable")}
	tab := &WorkspaceTab{ID: "billing", Ctrl: ctrl}
	app := &App{ctx: context.Background(), tabs: map[string]*WorkspaceTab{tab.ID: tab}, tabOrder: []string{tab.ID}, activeTabID: tab.ID}

	got := app.BalanceForTab(tab.ID)
	if got.Err == "" {
		t.Fatal("BalanceForTab error was not reported")
	}
	tab.telemMu.Lock()
	display := tab.runtimeCostDisplayCurrency
	tab.telemMu.Unlock()
	if display != "" {
		t.Fatalf("failed balance rebound display to %q", display)
	}
}

func nowUTC() time.Time { return time.Now().UTC() }
