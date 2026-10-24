package builtin

import (
	"reasonix/internal/contract/tool"
	"sync"
)

func NewStrictFileViews() *FileViews {
	v := NewFileViews()
	v.strict = true
	return v
}

// ForkFileViews keeps each child's observed versions separate from its parent.
func ForkFileViews(reg *tool.Registry) func(tool.Tool) tool.Tool {
	views := map[*FileViews]*FileViews{}
	var mu sync.Mutex
	bind := func(t tool.Tool) tool.Tool {
		parent := viewsOf(t)
		if parent == nil {
			return t
		}
		mu.Lock()
		defer mu.Unlock()
		child := views[parent]
		if child == nil {
			child = NewFileViews()
			child.strict = parent.strict
			views[parent] = child
		}
		return BindFileViews(t, child)
	}
	for _, name := range reg.Names() {
		if t, ok := reg.Get(name); ok {
			reg.Add(bind(t))
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
