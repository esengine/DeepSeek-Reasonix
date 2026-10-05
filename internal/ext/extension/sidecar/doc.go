// Package sidecar is the host half of Extension Protocol v2: it spawns
// extension sidecar processes, runs the initialize handshake, serves their
// Extension → Host calls (content reads, UI, provider streams), and owns
// their bounded shutdown and crash supervision.
//
// AUTHORIZATION INVARIANT: a sidecar may only ever be launched for a plugin
// package that is present in the pluginpkg installed state
// (<Reasonix home>/plugin-packages.json) AND currently enabled. The launch
// API (Manager.StartPackages) takes the pluginpkg installed state as its only
// input — there is no way to point it at an arbitrary binary or at a runtime
// declared by project config. Project configuration can declare MCP servers,
// hooks, and skills, but it can never declare a v2 runtime; keeping this
// invariant by construction is why StartPackages accepts a home directory and
// loads the state itself instead of accepting caller-supplied command specs.
//
// FULL-TRUST CONTRACT: a sidecar process inherits the UNFILTERED Reasonix
// environment (os.Environ), plus its manifest env and REASONIX_PLUGIN_ROOT /
// REASONIX_PLUGIN_NAME / REASONIX_PLUGIN_VERSION. Sidecars can read
// credentials, the session, and the workspace and can act with the user's
// full authority — the same contract an installed v2 runtime already accepted
// at install time (see pluginpkg.RuntimeTrustText). Sidecar stderr is redacted
// in this process layer; provider errors, structured UI, and interceptor
// reasons are redacted again by their host-side consumers. Ordinary
// provider/model content remains unchanged as product data.
package sidecar
