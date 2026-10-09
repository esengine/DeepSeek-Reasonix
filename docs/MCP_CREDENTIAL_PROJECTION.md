---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-03
---

# MCP credential projection

Operational config remains the authority for installation, approval hashing,
persistence and connections. Display and export consume copies.

## Structural rule

`internal/base/secrets` owns the rule. Keys are percent-decoded, normalized with
NFKC, split at camel-case boundaries and every non-letter/non-digit separator,
then Unicode-folded. Comparisons use complete components.

Sensitive components are token, password, passwd, secret, key, auth,
authorization, credential, signature, sig, session, cookie, bearer, jwt,
apikey, access and private. Credentials and separated pwd are also recognized;
bare PWD and OLDPWD preserve their working-directory meaning.

- URL userinfo is removed. Query and fragment keys and values are inspected,
  including values containing nested URLs or query strings. Keyed path
  credentials and opaque fragments are hidden.
- Headers hide the entire value of credential-bearing names, regardless of
  authentication scheme. Assignments and argument carriers use the same key
  rule. Header/env carrier flags hide their operands entirely.
- Invalid parses, ambiguous commands and depth exhaustion hide the whole
  affected value. Recursive projection is bounded to 32 levels.

## Diagnostics

Dependency prose is not a credential grammar. HTTP/SSE bodies, RPC messages,
subprocess tails and OAuth rejection descriptions never become diagnostic text.
Producers expose typed identity and bounded facts, such as status, RPC code and
the number of bytes read or retained.

`DiagnosticError` preserves `errors.Is` and `errors.As`. Trusted host producers
may implement `DiagnosticFacts`; implementations must contain only safe facts.
Unknown causes expose their Go type, never their message.

Subprocess stderr mirrors emit byte-count summaries. Startup stderr carries a
count through reconnect wrappers, never a second copy of dependency prose.

## Guards and scope

The sink registry checks projection owners and scans MCP consumers for new raw
endpoint/header/env formatting, including common local aliases. Inventory
consumers use already projected DTOs; operational fingerprints stay operational.

The deterministic generated corpus covers 512 seeds, structural carriers,
separators, encodings, Unicode folding, nesting and IPv6. Boundary tests keep
operational credentials separate from display projections.

Legacy transcript cleanup and extension UI text retain their existing contracts.
MCP producers do not use their text scrubbers. Opaque literals under ordinary
configuration keys cannot be distinguished from benign literals by this rule.
