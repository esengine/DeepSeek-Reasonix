// Package providerbroker keeps a remote session's model credentials on the
// machine the user configured them on, and off the host the agent runs on.
//
// A `reasonix serve` bootstrapped over SSH resolves providers from its own
// config, so every remote host needs the API key configured a second time and
// needs egress to the model endpoint. A broker inverts that: the local machine
// serves a Server on a loopback listener, an -R forward publishes it on the
// remote's loopback, and the remote kernel's provider.Resolver is a Client
// pointed at it. A Selection and a Request cross the tunnel; the provider is
// built and run at home, where the key already is.
//
// Running the provider at home rather than reverse-proxying its HTTP is what
// keeps the vendor judgements correct. Endpoint identity decides request
// shaping in dozens of places across the adapters — DeepSeek's beta prefix
// endpoint, Kimi's K3 contract, Gemini's model resource names, MiniMax's
// reasoning split — and every one of them reads a base URL. A remote whose
// base URL named 127.0.0.1 would take the default branch of each, silently.
//
// Errors cross as identities rather than sentences, because callers tell them
// apart: context-overflow compaction turns on *provider.APIError's status and
// the code inside its body, and a failure flattened to a string classifies as
// nothing. encodeError/decode round-trip the four provider error types so
// errors.As on the remote matches what the local provider actually returned.
package providerbroker
