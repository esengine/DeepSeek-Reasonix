package i18n

// UsageMessages is how /status words the session's ledger figures; Messages embeds it.
type UsageMessages struct {
	ChatStatusCacheWarmFmt   string // ledger hit rate with the cold start left out
	ChatStatusOutputRatioFmt string // output ÷ input percent, output tokens, input tokens
}
