// Package configbackup turns a user's global Reasonix setup into a snapshot
// that can leave the machine, and puts a chosen part of one back.
//
// A snapshot is a list of items, each owned by one category the user ticked
// at export: settings (providers, default model, interface), extensions
// (skills, plugin references, MCP servers), memory (user-level standing
// instructions and global facts), automation (hooks, the status line) and
// secrets (stored API keys, off unless asked for). Files travel as paths
// relative to a named root, never as this machine's absolute paths.
//
// The snapshot is sealed before it leaves: gzip, then XChaCha20-Poly1305 under
// a key derived from the user's passphrase with Argon2id. The envelope header
// carries the KDF parameters and is authenticated as associated data, so a
// server that stores the envelope sees only its size.
//
// Restoring is two steps. Plan decrypts, compares every item with what this
// machine has and caches the snapshot under a plan id; Apply takes that id,
// the items the user selected and, separately, the items whose consent the
// user gave. An item that runs code here (a hook, the status line, an MCP
// server) or that would send a stored key somewhere new needs consent, and
// Apply recomputes that requirement against the machine at apply time rather
// than trusting the plan. Plugins are never installed here: they come back as
// references for the ordinary plugin install flow and its own approval.
//
// Security posture — approval mode, permissions, sandbox, trust decisions —
// is deliberately not a category: restoring one would widen what the agent
// may do without the approval that widening normally takes.
package configbackup
