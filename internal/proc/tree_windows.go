//go:build windows

package proc

import (
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// TreeTracker records a process tree while a command is running. Windows Job
// Objects should own normal children, but Git Bash/MSYS launch chains can briefly
// expose grandchildren before or outside taskkill's live tree walk. Recording
// descendants gives cancellation a second chance to terminate those escapees.
type TreeTracker struct {
	root uint32
	done chan struct{}
	once sync.Once

	mu      sync.Mutex
	records map[uint32]processRecord
}

type processRecord struct {
	pid      uint32
	parent   uint32
	exe      string
	created  windows.Filetime
	hasTimes bool
}

func TrackTree(cmd *exec.Cmd) *TreeTracker {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	t := &TreeTracker{
		root:    uint32(cmd.Process.Pid),
		done:    make(chan struct{}),
		records: map[uint32]processRecord{},
	}
	t.record()
	go t.loop()
	return t
}

func (t *TreeTracker) Stop() {
	if t == nil {
		return
	}
	t.once.Do(func() { close(t.done) })
}

func (t *TreeTracker) Kill() int {
	if t == nil {
		return 0
	}
	t.record()
	records := t.snapshot()
	killed := 0
	for _, rec := range records {
		if rec.pid != t.root {
			killed += terminateRecord(rec)
		}
	}
	for _, rec := range records {
		if rec.pid == t.root {
			killed += terminateRecord(rec)
			break
		}
	}
	return killed
}

func (t *TreeTracker) loop() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			t.record()
		case <-t.done:
			return
		}
	}
}

func (t *TreeTracker) record() {
	if t == nil || t.root == 0 {
		return
	}
	records := processSnapshot()
	t.mu.Lock()
	if root, ok := records[t.root]; ok {
		t.records[t.root] = root
	}
	for _, rec := range descendantRecords(t.root, records) {
		t.records[rec.pid] = rec
	}
	t.mu.Unlock()
}

func (t *TreeTracker) snapshot() []processRecord {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]processRecord, 0, len(t.records))
	for _, rec := range t.records {
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].pid < out[j].pid })
	return out
}

func descendantRecords(root uint32, records map[uint32]processRecord) []processRecord {
	if root == 0 {
		return nil
	}
	children := map[uint32][]uint32{}
	for _, rec := range records {
		children[rec.parent] = append(children[rec.parent], rec.pid)
	}

	var out []processRecord
	seen := map[uint32]bool{root: true}
	var walk func(uint32)
	walk = func(pid uint32) {
		for _, child := range children[pid] {
			if child == 0 || seen[child] {
				continue
			}
			seen[child] = true
			if rec, ok := records[child]; ok {
				out = append(out, rec)
			}
			walk(child)
		}
	}
	walk(root)
	return out
}

func processSnapshot() map[uint32]processRecord {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer func() { _ = windows.CloseHandle(snap) }()

	records := map[uint32]processRecord{}
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	for err := windows.Process32First(snap, &pe); err == nil; err = windows.Process32Next(snap, &pe) {
		rec := processRecord{
			pid:    pe.ProcessID,
			parent: pe.ParentProcessID,
			exe:    strings.ToLower(windows.UTF16ToString(pe.ExeFile[:])),
		}
		rec.created, rec.hasTimes = processCreationTime(pe.ProcessID)
		records[rec.pid] = rec
	}
	return records
}

func processCreationTime(pid uint32) (windows.Filetime, bool) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return windows.Filetime{}, false
	}
	defer func() { _ = windows.CloseHandle(h) }()
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &created, &exited, &kernel, &user); err != nil {
		return windows.Filetime{}, false
	}
	return created, true
}

func terminateRecord(rec processRecord) int {
	if rec.pid == 0 {
		return 0
	}
	current, ok := processSnapshot()[rec.pid]
	if !ok || !sameProcessIdentity(rec, current) {
		return 0
	}
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, rec.pid)
	if err != nil {
		return 0
	}
	defer func() { _ = windows.CloseHandle(h) }()
	_ = windows.TerminateProcess(h, 1)
	return 1
}

func sameProcessIdentity(recorded, current processRecord) bool {
	if recorded.pid != current.pid {
		return false
	}
	if recorded.hasTimes && current.hasTimes {
		return recorded.created == current.created
	}
	if recorded.exe != "" && current.exe != "" {
		return strings.EqualFold(recorded.exe, current.exe)
	}
	return true
}
