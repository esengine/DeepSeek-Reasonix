package builtin

import "reasonix/internal/contract/tool"

// ForkFileViews gives direct and deferred file tools one new actor-owned record.
func ForkFileViews(reg *tool.Registry) func(tool.Tool) tool.Tool {
	views := NewFileViews()
	bind := func(t tool.Tool) tool.Tool {
		if viewsOf(t) == nil {
			return t
		}
		return BindFileViews(t, views)
	}
	if reg != nil {
		for _, name := range reg.Names() {
			if t, ok := reg.Get(name); ok {
				reg.Add(bind(t))
			}
		}
	}
	return bind
}

func viewsOf(t tool.Tool) *FileViews {
	switch x := t.(type) {
	case readFile:
		return x.views
	case writeFile:
		return x.views
	case editFile:
		return x.views
	case multiEdit:
		return x.views
	case deleteRange:
		return x.views
	case deleteSymbol:
		return x.views
	}
	return nil
}
