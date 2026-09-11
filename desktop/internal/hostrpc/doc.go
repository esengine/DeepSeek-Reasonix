// Package hostrpc is the Go side of the desktop host protocol
// (docs/DESKTOP_HOST_PROTOCOL.md): one JSON-RPC 2.0 connection over the
// service's stdio joining the Electron shell to the desktop App.
//
// Registry reflects over the exported methods of the bound App value the way
// the retired shell did, rejects any signature the shell could not call, and invokes a
// method from JSON arguments. Contract freezes the accepted commands, the
// event names and every DTO shape into canonical JSON whose SHA-256 digest
// both sides compare during the hello handshake; WriteTypeScript renders the
// same contract as the typed table the renderer compiles against.
//
// Server owns the wire: it gates every request behind desktop/hello,
// performs the protocol, contract, build and instance checks, routes
// lifecycle requests to Hooks, dispatches desktop/invoke through the
// registry, writes desktop/event notifications from one sequence, and issues
// host/* reverse requests. It imports no shell toolkit, so tests drive it
// through an in-memory rpcwire peer.
package hostrpc
