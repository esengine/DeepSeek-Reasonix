package event

// CacheDiagnostics describes whether and why the cacheable prefix changed since
// the last turn, and whether the messages this request carried over are the
// bytes the last one already sent. A miss with neither changed is the
// provider's. It rides on the Usage event so every frontend can show it.
type CacheDiagnostics struct {
	PrefixHash          string
	PrefixChanged       bool
	PrefixChangeReasons []string // "system", "tools", "log_rewrite", "body_unreported"
	SystemHash          string
	ToolsHash           string
	LogRewriteVersion   int
	ToolSchemaTokens    int
	CacheMissTokens     int
	CacheHitTokens      int
	CarriedMessages     int // messages both this request and the last one sent
	BodyChanged         bool
	BodyHash            string
}
