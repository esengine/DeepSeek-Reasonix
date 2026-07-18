package agent

// HasNativeSessionEventLog reports whether path has a non-empty Reasonix
// append-only event log. Shared history readers use this to distinguish the
// authoritative current transcript from legacy JSONL files that merely happen
// to have a similarly named sidecar.
func HasNativeSessionEventLog(path string) bool {
	probe, err := probeSessionEventLog(path)
	return err == nil && probe.native && probe.size > 0
}
