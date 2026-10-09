package config

import (
	"maps"
	"slices"
	"sync"
)

// A package parse reads every skill file of every enabled package, and a
// settings sheet asks for it from a dozen requests at once. Each caller waits
// for a parse that starts after it arrived, so the answer is never older than
// the call, but callers that arrive while one is running share the next one
// instead of queueing a parse each.
var packageFlights packageFlightSet

type packageFlightSet struct {
	mu    sync.Mutex
	homes map[string]*homeFlights
}

type homeFlights struct {
	running sync.Mutex
	pending *packageFlight
}

type packageFlight struct {
	done chan struct{}
	out  []InstalledPackage
}

func (s *packageFlightSet) load(home string, parse func(string) []InstalledPackage) []InstalledPackage {
	s.mu.Lock()
	if s.homes == nil {
		s.homes = map[string]*homeFlights{}
	}
	h := s.homes[home]
	if h == nil {
		h = &homeFlights{}
		s.homes[home] = h
	}
	f := h.pending
	leader := f == nil
	if leader {
		f = &packageFlight{done: make(chan struct{})}
		h.pending = f
	}
	s.mu.Unlock()

	if !leader {
		<-f.done
		return cloneInstalledPackages(f.out)
	}

	h.running.Lock()
	defer h.running.Unlock()
	s.mu.Lock()
	h.pending = nil
	s.mu.Unlock()
	defer close(f.done)
	f.out = parse(home)
	return cloneInstalledPackages(f.out)
}

func cloneInstalledPackages(in []InstalledPackage) []InstalledPackage {
	if in == nil {
		return nil
	}
	out := make([]InstalledPackage, len(in))
	for i, p := range in {
		p.Warnings = slices.Clone(p.Warnings)
		p.SkillRoots = slices.Clone(p.SkillRoots)
		p.AgentRoots = slices.Clone(p.AgentRoots)
		p.CommandDirs = slices.Clone(p.CommandDirs)
		if p.MCPServers != nil {
			servers := make(map[string]PackageMCPServer, len(p.MCPServers))
			for name, srv := range p.MCPServers {
				srv.Args = slices.Clone(srv.Args)
				srv.Env = maps.Clone(srv.Env)
				srv.Headers = maps.Clone(srv.Headers)
				servers[name] = srv
			}
			p.MCPServers = servers
		}
		out[i] = p
	}
	return out
}
