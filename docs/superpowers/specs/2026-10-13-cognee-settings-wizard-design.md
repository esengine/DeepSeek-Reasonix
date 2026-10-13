# Cognee Settings Wizard Design

## Goal

Let users configure and diagnose the Cognee project-memory MCP integration from Reasonix settings, without editing TOML or JSON by hand. The Cognee memory lifecycle remains inside Reasonix; this work adds an in-app setup and readiness surface and does not bundle the Cognee backend.

## Current behavior

Reasonix already performs bounded Cognee `recall` and verified `remember` operations through its standard MCP runtime. The runtime expects a configured server named `cognee`, an enabled `cognee-memory` Skill, an authorized and connected server, and compatible `recall` and `remember` schemas. When readiness checks fail, the turn continues and a generic notice says the service was not connected or authorized. MCP configuration, authentication, connection state, and Skill enablement currently live in separate settings surfaces.

## Design

Add a Cognee project-memory card to **Settings → Agent Skills**. The card combines Skill enablement with Cognee service readiness and provides a guided setup action. It reports distinct states for not configured, disabled, authorization required, connection failed, missing/incompatible tools, and ready. Failure detail comes from the existing MCP status/error surface; the card never displays credential values.

The setup flow creates or updates the fixed MCP server identity `cognee` and then uses Reasonix's existing MCP connect/auth lifecycle. It supports HTTPS endpoints and HTTP loopback endpoints for local services, matching the current Cognee endpoint policy. Existing stdio configurations remain manageable through the general MCP settings page. If an existing server is owned by another config source or plugin and cannot be safely edited here, the wizard links to its MCP details instead of replacing it.

An optional bearer token is entered in a password field and saved through Reasonix's credential store under a Cognee-specific environment key. The MCP config stores only an environment-variable reference. A blank token preserves a saved token when editing, and a server that needs no bearer token can be configured without one. Credentials must not appear in normal MCP config, UI status, logs, or error text.

After connection, Reasonix performs a readiness check that validates the presence and input schemas of `recall` and `remember`; it does not invoke either memory operation during setup. OAuth or other MCP authorization continues through the existing authorization actions. The readiness check must not start a second transport or bypass existing authorization.

## Scope and constraints

- Reuse existing MCP install/update, authorization, credential-store, and runtime identity checks.
- Keep Cognee memory opt-in through the existing `cognee-memory` Skill toggle.
- Preserve normal task fail-open behavior when Cognee is unavailable.
- Keep MCP `forget` and other Cognee write tools inaccessible through this feature.
- Do not bundle, install, or manage a local Cognee backend/database.
- Keep existing general MCP management and provider-visible tool schemas unchanged.

## Acceptance criteria

1. The Skills settings card explains readiness and distinguishes the expected failure states.
2. A user can configure an HTTPS or loopback HTTP Cognee endpoint from the card, optionally supply a bearer token, and connect using the existing MCP runtime.
3. The token is stored in the credential store; persisted MCP config contains only its environment reference.
4. Setup reports ready only when the server is connected/authorized and both required tool schemas are compatible; setup itself calls neither `recall` nor `remember`.
5. Existing or managed `cognee` entries are not silently overwritten; the user can navigate to their current MCP detail/editor when direct editing is unavailable.
6. An unavailable Cognee server still does not block normal task execution.

## Verification plan

Verify the readiness state mapping, endpoint restrictions, credential-reference persistence, authorization reuse, and that setup does not call memory tools. Run the desktop/frontend type checks and production build. Avoid changing or running the general test suites unless the user asks for tests.
