// Package docs exposes the product documentation bundled with each Reasonix
// build. Runtime consumers should retrieve focused sections instead of adding
// the full corpus to the provider-visible prompt.
package docs

import "embed"

// Content contains the Markdown documentation shipped by this exact build.
// Images and generated site assets are intentionally excluded from the agent
// retrieval corpus.
//
//go:embed *.md
var Content embed.FS
