package agentcomposition

import "fmt"

func (d Definition) validateModuleParameters() error {
	for _, s := range []Selection{d.MCP, d.Extensions} {
		for _, m := range s.Members {
			if _, err := ParseParameters(m.Parameters); err != nil {
				return err
			}
		}
	}
	for _, m := range d.Extensions.Members {
		p, err := ParseParameters(m.Parameters)
		if err != nil {
			return err
		}
		if p.URL != nil || p.Headers != nil || p.CallTimeoutSeconds != nil || p.StartupTimeoutSeconds != nil {
			return fmt.Errorf("%w: unsupported extension parameter", ErrInvalid)
		}
	}
	for _, m := range d.MCP.Members {
		p, err := ParseParameters(m.Parameters)
		if err != nil {
			return err
		}
		if p.TimeoutMillis != nil {
			return fmt.Errorf("%w: unsupported MCP parameter", ErrInvalid)
		}
	}
	for _, s := range []Selection{d.Tools, d.Skills, d.Hooks, d.Commands} {
		for _, m := range s.Members {
			if len(m.Parameters) > 0 && string(m.Parameters) != "{}" {
				return fmt.Errorf("%w: parameters unsupported for module %s", ErrInvalid, m.ID)
			}
		}
	}
	return nil
}
