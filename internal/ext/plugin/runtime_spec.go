package plugin

import "reflect"

// MCPRuntimeSpecMatches compares the complete host-local runtime behavior of
// two specs while deliberately excluding non-behavioral handles such as the
// stderr writer and LaunchManager pointer. Secret values are compared only in
// memory and are never serialized into diagnostics or provider-visible state.
func MCPRuntimeSpecMatches(a, b Spec) bool {
	return reflect.DeepEqual(mcpRuntimeSpecIdentityOf(a), mcpRuntimeSpecIdentityOf(b))
}
