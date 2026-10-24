package agent

import (
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/writeclaim"
)

// BindOptimisticWriters reserves named writes without narrowing opaque tools.
func BindOptimisticWriters(reg *tool.Registry, grant *writeclaim.WriteGrant, gate Gate, sched *writeclaim.SubagentScheduler, root string) *tool.Registry {
	bound := tool.NewRegistry()
	bound.CopyDisabledMCPFrom(reg)
	for _, name := range reg.Names() {
		writer, ok := reg.Get(name)
		if !ok {
			continue
		}
		if !writer.ReadOnly() && tool.WritesNamedPaths(writer) {
			writer = pathBoundWriter{inner: writer, grant: grant, gate: gate, sched: sched, workDir: root}
		}
		bound.Add(writer)
	}
	return bound
}
