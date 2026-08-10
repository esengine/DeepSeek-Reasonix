// Package sessioninbox is the durable session-level instruction queue.
//
// Each session owns a <stem>.inbox/ directory with a revisioned manifest
// (metadata only) and per-item PromptEnvelope blobs. Frontends admit work
// through control.Controller; this package never enters provider prompts.
package sessioninbox
