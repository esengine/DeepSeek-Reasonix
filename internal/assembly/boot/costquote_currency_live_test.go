package boot

import (
	"sync"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

func TestDisplayCurrencyNoticeRebindsTheQuoteTheSinkAttaches(t *testing.T) {
	var (
		mu      sync.Mutex
		quoted  []string
		billing = &provider.Pricing{CacheHit: 0.02, Input: 1, Output: 4, Currency: "¥"}
	)
	spy := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Usage && e.CostQuote != nil && e.CostQuote.Selected != nil {
			mu.Lock()
			quoted = append(quoted, e.CostQuote.Selected.Currency)
			mu.Unlock()
		}
	})
	sink := quotedSink(&config.Config{Providers: []config.ProviderEntry{{Name: "ds", Kind: "openai", BaseURL: "https://api.deepseek.com/v1", Model: "deepseek-flash", Models: []string{"deepseek-flash"}}}}, Options{Sink: spy})
	usage := event.Event{
		Kind: event.Usage, ModelRef: "ds/deepseek-flash",
		Usage:   &provider.Usage{PromptTokens: 1000, CompletionTokens: 1000, TotalTokens: 2000},
		Pricing: billing,
	}
	sink.Emit(event.Event{Kind: event.Notice, Code: event.NoticeCodeDisplayCurrency, Detail: "USD"})
	sink.Emit(usage)
	sink.Emit(event.Event{Kind: event.Notice, Code: event.NoticeCodeDisplayCurrency, Detail: "CNY"})
	sink.Emit(usage)

	mu.Lock()
	defer mu.Unlock()
	if len(quoted) != 2 || quoted[0] != "USD" || quoted[1] != "CNY" {
		t.Fatalf("rounds after a currency change were quoted in %v, want [USD CNY]", quoted)
	}
}
