// Package trustedstate is Trusted Host State (docs/design/TRUSTED_EXECUTION.md
// §2.3, §3.8): content-addressed objects plus one record chain per stream, each
// chain ending at a head file that names its newest record and generation.
//
// A chain proves nothing on its own. Anyone able to write the store can rewrite
// every record, recompute every digest and install a new head, and the result
// verifies. What the store can say is narrower: relative to a head this process
// already observed, the history did not change. Whether a head is out of the
// model's reach is not something the store can know, so the caller supplies the
// IntegrityLevel — the sandbox owns that answer — and every record carries the
// level it was written under.
//
// Objects are immutable and named by the SHA-256 of their bytes. A record is an
// object too; objects are published by a non-replacing hard link, so competing
// writers verify the winner. The head is the only file ever replaced, by
// rename so a crash leaves either the old head or the new one.
package trustedstate
