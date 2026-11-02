package stats

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/contract/provider"
)

const (
	uploadDirName  = "upload"
	uploadKeepDays = 400
	uploadFlushGap = 30 * time.Second
)

// uploadCount is what one provider was sent in one day.
type uploadCount struct {
	Bytes    int64 `json:"bytes"`
	Requests int64 `json:"requests"`
}

// uploadFile is one process's tally for one day. Each process writes only its
// own file, so concurrent processes never overwrite each other's counts and a
// reader adds the files up.
type uploadFile struct {
	Day       string                 `json:"day"`
	Providers map[string]uploadCount `json:"providers"`
}

// uploadMeter tallies request-body bytes per provider and day for this process.
type uploadMeter struct {
	dir      string
	instance string
	now      func() time.Time
	mu       sync.Mutex
	days     map[string]map[string]uploadCount
	dirty    map[string]bool
}

func newUploadMeter(dir, instance string, now func() time.Time) *uploadMeter {
	return &uploadMeter{dir: dir, instance: instance, now: now,
		days: map[string]map[string]uploadCount{}, dirty: map[string]bool{}}
}

// Add counts one request body sent by provider.
func (m *uploadMeter) Add(provider string, bytes int64) {
	if bytes <= 0 {
		return
	}
	day := m.now().Format(dayLayout)
	m.mu.Lock()
	defer m.mu.Unlock()
	counts := m.days[day]
	if counts == nil {
		counts = map[string]uploadCount{}
		m.days[day] = counts
	}
	c := counts[provider]
	c.Bytes += bytes
	c.Requests++
	counts[provider] = c
	m.dirty[day] = true
}

func (m *uploadMeter) path(day string) string {
	return filepath.Join(m.dir, uploadDirName, day+"."+m.instance+".json")
}

// Flush writes every day that changed since the last flush.
func (m *uploadMeter) Flush() error {
	m.mu.Lock()
	pending := map[string]uploadFile{}
	for day := range m.dirty {
		pending[day] = uploadFile{Day: day, Providers: maps.Clone(m.days[day])}
	}
	m.dirty = map[string]bool{}
	m.mu.Unlock()
	if len(pending) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Join(m.dir, uploadDirName), 0o700); err != nil {
		m.redirty(pending)
		return err
	}
	var failed error
	for day, file := range pending {
		raw, err := json.Marshal(file)
		if err == nil {
			err = fileutil.AtomicWriteFile(m.path(day), raw, 0o600)
		}
		if err != nil {
			failed = err
			m.redirty(map[string]uploadFile{day: file})
		}
	}
	return failed
}

func (m *uploadMeter) redirty(days map[string]uploadFile) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for day := range days {
		m.dirty[day] = true
	}
}

// pruneBefore removes tally files for days before cutoff.
func (m *uploadMeter) pruneBefore(cutoff time.Time) {
	entries, err := os.ReadDir(filepath.Join(m.dir, uploadDirName))
	if err != nil {
		return
	}
	limit := cutoff.Format(dayLayout)
	for _, e := range entries {
		day, ok := uploadFileDay(e.Name())
		if ok && day < limit {
			_ = os.Remove(filepath.Join(m.dir, uploadDirName, e.Name()))
		}
	}
}

// uploadFileDay reads the day out of "<day>.<instance>.json".
func uploadFileDay(name string) (string, bool) {
	day, rest, ok := strings.Cut(name, ".")
	if !ok || !strings.HasSuffix(rest, ".json") {
		return "", false
	}
	if _, err := time.Parse(dayLayout, day); err != nil {
		return "", false
	}
	return day, true
}

var uploadMeters = struct {
	sync.Mutex
	current *uploadMeter
	ticking bool
}{}

// StartUploadMeter begins counting the request bodies this process sends to
// model providers into dir. Counts reach disk every half minute and are not
// lost to a crash beyond that; asking again for the same dir changes nothing.
func StartUploadMeter(dir string) {
	if dir == "" {
		return
	}
	uploadMeters.Lock()
	defer uploadMeters.Unlock()
	if cur := uploadMeters.current; cur != nil {
		if cur.dir == dir {
			return
		}
		_ = cur.Flush()
	}
	m := newUploadMeter(dir, fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano()), time.Now)
	m.pruneBefore(m.now().AddDate(0, 0, -uploadKeepDays))
	uploadMeters.current = m
	provider.SetUploadObserver(m.Add)
	if !uploadMeters.ticking {
		uploadMeters.ticking = true
		go func() {
			tick := time.NewTicker(uploadFlushGap)
			defer tick.Stop()
			for range tick.C {
				FlushUploadMeter()
			}
		}()
	}
}

// FlushUploadMeter writes the running meter's counts to disk now.
func FlushUploadMeter() {
	uploadMeters.Lock()
	cur := uploadMeters.current
	uploadMeters.Unlock()
	if cur != nil {
		_ = cur.Flush()
	}
}

// UploadDay is one day's total in an upload series.
type UploadDay struct {
	Day   string `json:"day"`
	Bytes int64  `json:"bytes"`
}

// UploadProvider is one provider's share of the range.
type UploadProvider struct {
	Provider string  `json:"provider"`
	Bytes    int64   `json:"bytes"`
	Requests int64   `json:"requests"`
	Percent  float64 `json:"percent"`
}

// UploadStats is what model providers were sent over a range: request bodies
// as they went out, before the connection adds its own overhead.
type UploadStats struct {
	From      string           `json:"from"`
	To        string           `json:"to"`
	Bytes     int64            `json:"bytes"`
	Requests  int64            `json:"requests"`
	Daily     []UploadDay      `json:"daily"`
	Providers []UploadProvider `json:"providers"`
}

// QueryUpload adds up every process's tally for the days in [from, to].
func QueryUpload(dir string, from, to time.Time) (UploadStats, error) {
	days := daysInRange(from, to)
	out := UploadStats{Daily: []UploadDay{}, Providers: []UploadProvider{}}
	if len(days) == 0 {
		return out, nil
	}
	out.From, out.To = days[0], days[len(days)-1]
	perDay := map[string]int64{}
	perProvider := map[string]uploadCount{}
	entries, err := os.ReadDir(filepath.Join(dir, uploadDirName))
	if err != nil && !os.IsNotExist(err) {
		return out, err
	}
	for _, e := range entries {
		day, ok := uploadFileDay(e.Name())
		if !ok || day < out.From || day > out.To {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, uploadDirName, e.Name()))
		if err != nil {
			continue
		}
		var file uploadFile
		if json.Unmarshal(raw, &file) != nil {
			continue
		}
		for name, c := range file.Providers {
			perDay[day] += c.Bytes
			total := perProvider[name]
			total.Bytes += c.Bytes
			total.Requests += c.Requests
			perProvider[name] = total
			out.Bytes += c.Bytes
			out.Requests += c.Requests
		}
	}
	for _, day := range days {
		out.Daily = append(out.Daily, UploadDay{Day: day, Bytes: perDay[day]})
	}
	for name, c := range perProvider {
		share := UploadProvider{Provider: name, Bytes: c.Bytes, Requests: c.Requests}
		if out.Bytes > 0 {
			share.Percent = float64(c.Bytes) * 100 / float64(out.Bytes)
		}
		out.Providers = append(out.Providers, share)
	}
	sort.Slice(out.Providers, func(i, j int) bool {
		if out.Providers[i].Bytes != out.Providers[j].Bytes {
			return out.Providers[i].Bytes > out.Providers[j].Bytes
		}
		return out.Providers[i].Provider < out.Providers[j].Provider
	})
	return out, nil
}
