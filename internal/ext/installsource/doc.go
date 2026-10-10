// Package installsource implements the `install_source` tool: a two-phase
// installer for Reasonix skills and MCP servers. A single call resolves a
// source (URL, local file/folder, .mcp.json, package name, or local executable)
// into a deterministic plan. When the caller sets apply=true, any registered
// ApprovalFunc may still deny that exact plan before writes or MCP connects run.
//
// The two-phase design exists so the model (or a UI) can inspect a plan before
// it touches disk or spawns subprocesses. `install_source` deliberately does
// not run a README's `curl | sh` chain: it locates a concrete manifest
// (SKILL.md / <name>.md / <name>/SKILL.md / nested skill roots / .mcp.json /
// mcpServers entry) and describes what it would do, and only then does it act on
// apply=true. Single skills are written to the canonical <name>/SKILL.md layout;
// flat <name>.md is treated as compatibility input.
//
// Preview text: every string and list a plan shows is projected once, in
// preview.go, so each frontend inherits it. Names, paths, commands and
// arguments render hidden characters as visible \u{hex} escapes; prose has
// them removed. Per field: identity 128 graphemes, locator 1024, prose 600 and
// 6 lines; lists 100, 64 and 50 items. A cut ends the field with an ellipsis and sets
// previewTruncated. Beyond 50 non-high-risk actions the rest are counted in
// hiddenActions; every high-risk action is always listed. The planId digests the complete unprojected plan,
// so what is approved is the plan itself, never the text shown.
//
// Concurrency: each Execute call is independent; the tool does not lock the
// filesystem. Callers that want to serialize installs (e.g. two parallel calls
// for the same skill name) should do so in the host.
package installsource
