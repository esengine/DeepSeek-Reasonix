//go:build windows

package proc

import (
	"os"
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
	root       uint32
	done       chan struct{}
	once       sync.Once
	freezeOnce sync.Once

	mu      sync.Mutex
	records map[uint32]processRecord
	frozen  bool
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
	root, ok := processRecordFromHandle(cmd.Process, uint32(cmd.Process.Pid))
	if !ok {
		// Without an immutable creation-time identity, a later retry could
		// mistake a reused PID for the original root. The handle-pinned taskkill
		// pass remains available while the process itself is still waitable.
		return nil
	}
	t := &TreeTracker{
		root:    uint32(cmd.Process.Pid),
		done:    make(chan struct{}),
		records: map[uint32]processRecord{root.pid: root},
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
	t.freeze()
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

// freeze captures one final live-tree snapshot, then makes the identity set
// immutable. Retry kills can act on those same process objects, but can never
// reinterpret a reused PID or discover an unrelated new root generation.
func (t *TreeTracker) freeze() {
	if t == nil {
		return
	}
	t.freezeOnce.Do(func() {
		t.record()
		t.mu.Lock()
		t.frozen = true
		t.mu.Unlock()
		t.Stop()
	})
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
	t.recordSnapshot(processSnapshot())
}

// recordSnapshot extends the immutable identity set only while the original
// root identity is still present. It never replaces an existing PID record:
// after a process exits, that numeric PID can be reused by an unrelated tree.
func (t *TreeTracker) recordSnapshot(records map[uint32]processRecord) {
	if t == nil || t.root == 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.frozen {
		return
	}
	root, tracked := t.records[t.root]
	currentRoot, live := records[t.root]
	if !tracked || !live || !sameProcessIdentity(root, currentRoot) {
		return
	}
	for _, rec := range descendantRecords(t.root, records) {
		if _, exists := t.records[rec.pid]; !exists && rec.hasTimes {
			t.records[rec.pid] = rec
		}
	}
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

func processRecordFromHandle(process *os.Process, pid uint32) (processRecord, bool) {
	if process == nil || pid == 0 {
		return processRecord{}, false
	}
	record := processRecord{pid: pid}
	var queryErr error
	err := process.WithHandle(func(rawHandle uintptr) {
		var exited, kernel, user windows.Filetime
		queryErr = windows.GetProcessTimes(
			windows.Handle(rawHandle), &record.created, &exited, &kernel, &user,
		)
	})
	if err != nil || queryErr != nil {
		return processRecord{}, false
	}
	record.hasTimes = true
	return record, true
}

func terminateRecord(rec processRecord) int {
	if rec.pid == 0 || !rec.hasTimes {
		return 0
	}
	h, err := windows.OpenProcess(
		windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION,
		false,
		rec.pid,
	)
	if err != nil {
		return 0
	}
	defer func() { _ = windows.CloseHandle(h) }()
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &created, &exited, &kernel, &user); err != nil || created != rec.created {
		return 0
	}
	if err := windows.TerminateProcess(h, 1); err != nil {
		return 0
	}
	return 1
}

func sameProcessIdentity(recorded, current processRecord) bool {
	if recorded.pid != current.pid {
		return false
	}
	if recorded.hasTimes || current.hasTimes {
		return recorded.hasTimes && current.hasTimes && recorded.created == current.created
	}
	if recorded.exe != "" && current.exe != "" {
		return strings.EqualFold(recorded.exe, current.exe)
	}
	return true
}
