// Package releasenotes exposes the reviewed release catalog bundled with each
// Reasonix build.
package releasenotes

import "embed"

// Content contains the structured release history from the exact source
// revision used to build the product.
//
//go:embed releases.json
var Content embed.FS
