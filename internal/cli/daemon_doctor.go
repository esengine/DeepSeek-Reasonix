package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/daemon"
)

type daemonDoctorCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // ok|warn|fail
	Message string `json:"message,omitempty"`
}

type daemonDoctorRuntimeSummary struct {
	Total         int        `json:"total"`
	Corrupt       int        `json:"corrupt"`
	ActiveGoals   int        `json:"active_goals"`
	Running       int        `json:"running"`
	Interrupted   int        `json:"interrupted"`
	Waiting       int        `json:"waiting"`
	Scheduled     int        `json:"scheduled"`
	Watched       int        `json:"watched"`
	Budgeted      int        `json:"budgeted"`
	BudgetBlocked int        `json:"budget_blocked"`
	LastUpdated   *time.Time `json:"last_updated,omitempty"`
}

type daemonDoctorOnlineStatus struct {
	Reachable bool   `json:"reachable"`
	Status    string `json:"status,omitempty"`
	Addr      string `json:"addr,omitempty"`
	PID       int    `json:"pid,omitempty"`
	Sessions  int    `json:"sessions,omitempty"`
	Uptime    string `json:"uptime,omitempty"`
	Error     string `json:"error,omitempty"`
}

type daemonDoctorReport struct {
	SessionDir string                     `json:"session_dir"`
	Addr       string                     `json:"addr"`
	LogFile    string                     `json:"log_file,omitempty"`
	Checks     []daemonDoctorCheck        `json:"checks"`
	Runtime    daemonDoctorRuntimeSummary `json:"runtime"`
	Online     daemonDoctorOnlineStatus   `json:"online"`
}

func daemonDoctor(args []string) int {
	fs := flag.NewFlagSet("daemon doctor", flag.ContinueOnError)
	addr := fs.String("addr", daemon.DefaultAddr, "daemon 地址")
	dir := fs.String("dir", "", "会话目录（用于读取本地 token）")
	logFile := fs.String("log-file", "", "daemon 日志文件（默认 <session-dir>/.daemon.log，none 表示跳过）")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	report, failed := buildDaemonDoctorReport(*addr, *dir, *logFile, daemonGet)
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
	} else {
		printDaemonDoctorReport(report)
	}
	if failed {
		return 1
	}
	return 0
}

type daemonDoctorHTTPGet func(addr, dir, path string) (*http.Response, error)

func buildDaemonDoctorReport(addr, dir, logFile string, get daemonDoctorHTTPGet) (daemonDoctorReport, bool) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		dir = config.SessionDir()
	}
	report := daemonDoctorReport{
		SessionDir: dir,
		Addr:       addr,
		LogFile:    resolveDaemonLogFile(dir, logFile),
	}

	report.checkSessionDir()
	report.checkToken()
	report.checkLock()
	report.checkLogFile()
	report.Runtime = report.scanRuntimeSidecars()
	report.checkRuntimeSummary()
	report.Online = report.checkOnline(get)

	failed := false
	for _, check := range report.Checks {
		if check.Status == "fail" {
			failed = true
			break
		}
	}
	return report, failed
}

func (r *daemonDoctorReport) addCheck(name, status, message string) {
	r.Checks = append(r.Checks, daemonDoctorCheck{Name: name, Status: status, Message: message})
}

func (r *daemonDoctorReport) checkSessionDir() {
	info, err := os.Stat(r.SessionDir)
	switch {
	case err == nil && info.IsDir():
		r.addCheck("session_dir", "ok", r.SessionDir)
	case err == nil:
		r.addCheck("session_dir", "fail", "path exists but is not a directory")
	case os.IsNotExist(err):
		r.addCheck("session_dir", "fail", "directory does not exist")
	default:
		r.addCheck("session_dir", "fail", err.Error())
	}
}

func (r *daemonDoctorReport) checkToken() {
	path := daemon.TokenFile(r.SessionDir)
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			r.addCheck("token", "warn", "token file missing; daemon start will create it")
			return
		}
		r.addCheck("token", "fail", err.Error())
		return
	}
	if strings.TrimSpace(string(b)) == "" {
		r.addCheck("token", "fail", "token file is empty")
		return
	}
	if info, err := os.Stat(path); err == nil {
		if mode := info.Mode().Perm(); mode&0o077 != 0 {
			r.addCheck("token", "warn", fmt.Sprintf("token file permissions are too broad (%04o); expected 0600", mode))
			return
		}
	}
	r.addCheck("token", "ok", "token file is readable")
}

func (r *daemonDoctorReport) checkLock() {
	path := daemon.LockFile(r.SessionDir)
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			r.addCheck("lock", "ok", "no lock file")
			return
		}
		r.addCheck("lock", "fail", err.Error())
		return
	}
	raw := strings.TrimSpace(string(b))
	pid, err := strconv.Atoi(raw)
	if err != nil || pid <= 0 {
		r.addCheck("lock", "warn", "lock file exists but PID is invalid")
		return
	}
	if daemonDoctorProcessAlive(pid) {
		r.addCheck("lock", "ok", fmt.Sprintf("daemon-like process is alive with pid %d", pid))
		return
	}
	r.addCheck("lock", "warn", fmt.Sprintf("stale lock file for pid %d", pid))
}

func (r *daemonDoctorReport) checkLogFile() {
	if strings.TrimSpace(r.LogFile) == "" {
		r.addCheck("log", "ok", "file logging disabled")
		return
	}
	info, err := os.Stat(r.LogFile)
	switch {
	case err == nil && info.IsDir():
		r.addCheck("log", "fail", "log path exists but is a directory")
	case err == nil:
		if mode := info.Mode().Perm(); mode&0o222 == 0 {
			r.addCheck("log", "fail", fmt.Sprintf("log file is not writable (%04o)", mode))
			return
		}
		f, err := os.OpenFile(r.LogFile, os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			r.addCheck("log", "fail", err.Error())
			return
		}
		_ = f.Close()
		r.addCheck("log", "ok", fmt.Sprintf("log file writable (%d bytes)", info.Size()))
	case os.IsNotExist(err):
		if dirInfo, dirErr := os.Stat(filepath.Dir(r.LogFile)); dirErr == nil && dirInfo.IsDir() && dirInfo.Mode().Perm()&0o222 == 0 {
			r.addCheck("log", "fail", fmt.Sprintf("log directory is not writable (%04o)", dirInfo.Mode().Perm()))
			return
		}
		r.addCheck("log", "warn", "log file missing; daemon start will create it")
	default:
		r.addCheck("log", "fail", err.Error())
	}
}

func daemonDoctorProcessAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

func (r *daemonDoctorReport) scanRuntimeSidecars() daemonDoctorRuntimeSummary {
	var summary daemonDoctorRuntimeSummary
	for _, sessionPath := range daemonDoctorSessionPaths(r.SessionDir) {
		meta, ok, err := agent.LoadRuntimeMeta(sessionPath)
		if err != nil || !ok {
			summary.Corrupt++
			continue
		}
		summary.Total++
		if !meta.UpdatedAt.IsZero() && (summary.LastUpdated == nil || meta.UpdatedAt.After(*summary.LastUpdated)) {
			updated := meta.UpdatedAt
			summary.LastUpdated = &updated
		}
		if meta.Goal.Text != "" && (meta.Goal.Status == "running" || meta.Goal.Status == "blocked") {
			summary.ActiveGoals++
		}
		if agent.IsRunInFlight(meta.Run.Status) {
			summary.Running++
		}
		if meta.Run.Status == "interrupted" {
			summary.Interrupted++
		}
		if meta.Wait.Kind != "" || strings.HasPrefix(meta.Run.Status, "waiting_") {
			summary.Waiting++
		}
		if meta.Scheduler.Enabled || meta.Scheduler.DailyAt != "" || meta.Scheduler.Interval > 0 {
			summary.Scheduled++
		}
		if meta.FileWatch.Enabled || len(meta.FileWatch.Paths) > 0 {
			summary.Watched++
		}
		if meta.Budget.DailyWakeupLimit > 0 || meta.Budget.MaxGoalAutoTurns > 0 ||
			meta.Budget.DailyModelCallLimit > 0 || meta.Budget.DailyModelCostLimit > 0 {
			summary.Budgeted++
		}
		if meta.Budget.LastBlockedReason != "" {
			summary.BudgetBlocked++
		}
	}
	return summary
}

func daemonDoctorSessionPaths(sessionDir string) []string {
	var paths []string
	addMatches := func(dir string) {
		matches, _ := filepath.Glob(filepath.Join(dir, "*.runtime.json"))
		for _, runtimePath := range matches {
			paths = append(paths, strings.TrimSuffix(runtimePath, ".runtime.json"))
		}
	}
	addMatches(sessionDir)
	projectsDir := filepath.Join(filepath.Dir(sessionDir), "projects")
	entries, err := os.ReadDir(projectsDir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				addMatches(filepath.Join(projectsDir, entry.Name(), "sessions"))
			}
		}
	}
	sort.Strings(paths)
	return paths
}

func (r *daemonDoctorReport) checkRuntimeSummary() {
	msg := fmt.Sprintf("runtime sidecars=%d active_goals=%d scheduled=%d watched=%d budgeted=%d budget_blocked=%d waiting=%d interrupted=%d",
		r.Runtime.Total, r.Runtime.ActiveGoals, r.Runtime.Scheduled, r.Runtime.Watched, r.Runtime.Budgeted, r.Runtime.BudgetBlocked, r.Runtime.Waiting, r.Runtime.Interrupted)
	if r.Runtime.Corrupt > 0 {
		r.addCheck("runtime", "fail", fmt.Sprintf("%s corrupt=%d", msg, r.Runtime.Corrupt))
		return
	}
	r.addCheck("runtime", "ok", msg)
}

func (r *daemonDoctorReport) checkOnline(get daemonDoctorHTTPGet) daemonDoctorOnlineStatus {
	if get == nil {
		r.addCheck("online", "warn", "online status not checked")
		return daemonDoctorOnlineStatus{}
	}
	resp, err := get(r.Addr, r.SessionDir, "/status")
	if err != nil {
		r.addCheck("online", "warn", "daemon not reachable: "+err.Error())
		if daemonDoctorPortAcceptsTCP(r.Addr) {
			r.addCheck("port", "warn", "address accepts TCP but daemon /status is not reachable; port may be occupied or token may be stale")
		}
		return daemonDoctorOnlineStatus{Reachable: false, Error: err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		msg := fmt.Sprintf("daemon returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
		r.addCheck("online", "fail", msg)
		return daemonDoctorOnlineStatus{Reachable: true, Error: msg}
	}
	var status daemon.StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		r.addCheck("online", "fail", "invalid status response: "+err.Error())
		return daemonDoctorOnlineStatus{Reachable: true, Error: err.Error()}
	}
	r.addCheck("online", "ok", fmt.Sprintf("daemon %s pid=%d sessions=%d uptime=%s", status.Status, status.PID, status.Sessions, status.Uptime))
	return daemonDoctorOnlineStatus{
		Reachable: true,
		Status:    status.Status,
		Addr:      status.Addr,
		PID:       status.PID,
		Sessions:  status.Sessions,
		Uptime:    status.Uptime,
	}
}

func daemonDoctorPortAcceptsTCP(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func printDaemonDoctorReport(report daemonDoctorReport) {
	fmt.Println("daemon doctor")
	fmt.Printf("session_dir: %s\n", report.SessionDir)
	fmt.Printf("addr: %s\n", report.Addr)
	if report.LogFile != "" {
		fmt.Printf("log_file: %s\n", report.LogFile)
	}
	for _, check := range report.Checks {
		line := fmt.Sprintf("[%s] %s", check.Status, check.Name)
		if check.Message != "" {
			line += ": " + check.Message
		}
		fmt.Println(line)
	}
	if report.Runtime.LastUpdated != nil {
		fmt.Printf("last_runtime_update: %s\n", report.Runtime.LastUpdated.Local().Format("2006-01-02 15:04:05"))
	}
}
