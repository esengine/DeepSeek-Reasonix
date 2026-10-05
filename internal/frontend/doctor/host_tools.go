package doctor

import (
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/usecap"
)

// hostToolContracts are the tools every assembled registry carries, read from
// their owners: the compile-time built-ins and the use_capability proxy that
// assembly adds beside them.
func hostToolContracts() []tool.ContractEntry {
	return append(tool.BuiltinContractEntries(), tool.ContractEntry{Name: new(usecap.UseCapabilityTool).Name()})
}
