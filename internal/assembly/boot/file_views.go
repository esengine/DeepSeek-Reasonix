package boot

import (
	"reasonix/internal/contract/tool"
	"reasonix/internal/tools/builtin"
)

// fileViewTools are the tools that read or write whole files, and so record or
// check what the agent last saw of one.
var fileViewTools = []string{"read_file", "write_file", "edit_file", "multi_edit", "delete_range", "delete_symbol"}

// bindFileViews gives this run's file tools one shared record of what the agent
// last saw of each file, so write_file can refuse to discard a change made since.
func bindFileViews(reg *tool.Registry, protect bool) {
	if !protect {
		return
	}
	views := builtin.NewFileViews()
	for _, name := range fileViewTools {
		if t, ok := reg.Get(name); ok {
			reg.Add(builtin.BindFileViews(t, views))
		}
	}
}
