package ansi

// ParseOption specifies parse option.
type ParseOption struct {
	ignoreUnexpectedCode bool
	ansiForegroundColor  string
	ansiBackgroundColor  string
}

// WithIgnoreInvalidCodes disables returning an error on invalid ANSI code.
func WithIgnoreInvalidCodes() ParseOption {
	return ParseOption{ignoreUnexpectedCode: true}
}

// WithDefaultForegroundColor specifies default foreground code (ANSI 39).
// See ColourMap variable and foreground color codes 30-37.
func WithDefaultForegroundColor(ansiColor string) ParseOption {
	return ParseOption{ansiForegroundColor: ansiColor}
}

// WithDefaultBackgroundColor specifies default foreground code (ANSI 49).
// See ColourMap variable and foreground color codes 30-37.
func WithDefaultBackgroundColor(ansiColor string) ParseOption {
	return ParseOption{ansiBackgroundColor: ansiColor}
}
