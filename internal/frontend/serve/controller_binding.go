package serve

import (
	"errors"

	"reasonix/internal/session/control"
)

type projectBindings struct {
	dispatch control.ProjectDispatcher
	open     control.ProjectOpener
}

func (p projectBindings) bind(c *control.Controller) {
	c.SetProjectDispatcher(p.dispatch)
	c.SetProjectOpener(p.open)
}

func (s *Server) publishController(previous control.SessionAPI, next *control.Controller) error {
	s.mu.RLock()
	current, group, projects := s.ctrl, s.currency, s.projects
	s.mu.RUnlock()
	if current != previous {
		return errors.New("session changed during controller replacement")
	}
	old, _ := previous.(*control.Controller)
	if group != nil {
		if err := group.Bind(old, next, s.bc.SetDisplayCurrency); err != nil {
			return err
		}
	}
	s.mu.Lock()
	if s.ctrl == previous {
		projects.bind(next)
		s.ctrl = next
		s.mu.Unlock()
		s.bindCompositionSelector(next)
		return nil
	}
	current = s.ctrl
	s.mu.Unlock()
	err := errors.New("session changed during controller replacement")
	if group != nil {
		restored, _ := current.(*control.Controller)
		err = errors.Join(err, group.Bind(next, restored, s.bc.SetDisplayCurrency))
	}
	return err
}
