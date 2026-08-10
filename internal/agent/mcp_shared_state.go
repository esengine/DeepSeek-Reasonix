package agent

import (
	"strings"

	"reasonix/internal/plugin"
)

// The live-connection view every frontend on one session observes. It is
// deliberately shared: the Host and its processes are shared at the same scope.

func (s *mcpProxySharedState) markConnected(server string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.connected == nil {
		s.connected = map[string]bool{}
	}
	s.connected[server] = true
}

func (s *mcpProxySharedState) setLiveTools(server string, snap []plugin.CachedTool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.liveTools == nil {
		s.liveTools = map[string][]plugin.CachedTool{}
	}
	s.liveTools[server] = snap
	if s.connected == nil {
		s.connected = map[string]bool{}
	}
	s.connected[server] = true
}

func (s *mcpProxySharedState) snapshotLiveTools() map[string][]plugin.CachedTool {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.liveTools) == 0 {
		return nil
	}
	out := make(map[string][]plugin.CachedTool, len(s.liveTools))
	for k, v := range s.liveTools {
		out[k] = append([]plugin.CachedTool(nil), v...)
	}
	return out
}

func (s *mcpProxySharedState) clearServer(server string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	delete(s.connected, strings.TrimSpace(server))
	delete(s.liveTools, strings.TrimSpace(server))
	s.mu.Unlock()
}
