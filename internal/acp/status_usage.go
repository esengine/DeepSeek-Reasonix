package acp

import "reasonix/internal/billing"

// ReasonixUsage reports inclusive totals alongside cache and reasoning subsets.
type ReasonixUsage struct {
	TotalTokens      int                `json:"totalTokens"`
	PromptTokens     int                `json:"promptTokens"`
	CompletionTokens int                `json:"completionTokens"`
	ReasoningTokens  int                `json:"reasoningTokens"`
	CacheHitTokens   int                `json:"cacheHitTokens"`
	CacheMissTokens  int                `json:"cacheMissTokens"`
	Estimated        bool               `json:"estimated,omitempty"`
	CacheHitRatio    *float64           `json:"cacheHitRatio"`
	EstimatedCost    *float64           `json:"estimatedCost"`
	Currency         *string            `json:"currency"`
	CostComplete     *bool              `json:"costComplete,omitempty"`
	DisplayComplete  *bool              `json:"displayComplete,omitempty"`
	DisplayStatus    string             `json:"displayStatus,omitempty"`
	AggregateMode    string             `json:"aggregateMode,omitempty"`
	OriginalTotals   []billing.Money    `json:"originalTotals,omitempty"`
	CostQuote        *billing.CostQuote `json:"costQuote,omitempty"`
	UsageSource      string             `json:"usageSource"`
}

type ReasonixStatusUsage struct {
	Turn       ReasonixUsage `json:"turn"`
	Cumulative ReasonixUsage `json:"cumulative"`
}
