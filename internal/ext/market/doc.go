// Package market reads the Reasonix community registry and installs what it
// lists through install_source. It is a source, never a trust root:
//
//   - The registry is reached over https at one fixed host, with a timeout, a
//     body cap and no redirects. Its rows are untrusted data.
//   - Browsing keeps the last good anonymous answer on disk and serves it,
//     marked with when it was saved, only when the registry cannot answer.
//     Install, publish, vote and a signed-in view never read or write it. The
//     cache is not a trust source: whoever can write the user's cache can only
//     forge what is displayed (the pinned badge included); the install digest
//     chain never reads it, and an answer marked private or no-store is not kept.
//   - Two things are written back. A vote carries the account token, which
//     therefore only ever goes to that host. An install report carries the
//     slug and a random id kept only for the market (InstallID) — no token,
//     no content — and is sent only when the host passes that id, which it
//     does only with anonymous usage statistics switched on.
//   - Only an approved version is installable. When its row carries a content
//     digest a reviewer bound to it, that digest reaches install_source as
//     expectDigest, which refuses material that differs before writing. One
//     without a digest installs only on the person's explicit trust, pinned to
//     the digest of the preview they confirmed, and says it was unreviewed.
//   - Every install is the ordinary two-phase plan and apply. Apply must echo
//     the planId of a plan it was shown, so a source that expands into several
//     skills is listed to the person before any of them lands.
//   - What was installed from here is recorded in a ledger under the Reasonix
//     home, read back only to say "installed" — and only while the recorded
//     targets still exist.
//   - Publishing sends the account token to that same host and nowhere else,
//     and refuses a submission installable would refuse once approved. A theme
//     is its own category installed as a plugin package that carries only themes.
//   - A publisher may install their own package in any review state. Which one
//     and from where is the registry's answer for that account; with no
//     reviewer's digest, the confirmed preview's digest is the pin, and the
//     outcome and the ledger both say the install was unreviewed.
package market
