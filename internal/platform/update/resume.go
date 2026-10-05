package update

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// ErrStalled is a download connection that delivered nothing for the
// transport's StallTimeout. It is transient: the next attempt resumes.
var ErrStalled = errors.New("update: the download stalled")

// ErrSizeMismatch is an address that answered with a body the manifest's size
// rules out: an error page served as 200, or more than the artifact holds. It
// is that address's verdict, so the next address is asked rather than this one
// again, and what it sent is discarded rather than resumed.
var ErrSizeMismatch = errors.New("update: the download does not match the manifest's size")

// errRangeReset is a resume the server could not satisfy from what was kept.
// It is transient: what was kept is discarded and the next attempt starts over.
var errRangeReset = errors.New("update: the kept download does not match the served one")

// sink is where a download's bytes land: memory, or a partial file that a
// later download resumes from.
type sink interface {
	io.Writer
	Size() int64
	Reset() error
}

type memorySink struct{ bytes.Buffer }

func (m *memorySink) Size() int64  { return int64(m.Len()) }
func (m *memorySink) Reset() error { m.Buffer.Reset(); return nil }

// fileSink writes to a partial file. A failed write is the disk's, never the
// network's, so it is classed ErrStore and not retried.
type fileSink struct {
	f *os.File
	n int64
}

func (s *fileSink) Write(b []byte) (int, error) {
	n, err := s.f.Write(b)
	s.n += int64(n)
	if err != nil {
		return n, fmt.Errorf("%w: %w", ErrStore, err)
	}
	return n, nil
}

func (s *fileSink) Size() int64 { return s.n }

func (s *fileSink) Reset() error {
	if err := s.f.Truncate(0); err != nil {
		return fmt.Errorf("%w: %w", ErrStore, err)
	}
	s.n = 0
	if _, err := s.f.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("%w: %w", ErrStore, err)
	}
	return nil
}

// Download fetches an artifact, resuming from what it already holds when a
// retry is needed. expectedSize is the manifest's size: 0 leaves it unbounded
// up to MaxAssetSize.
func (t Transport) Download(ctx context.Context, url string, expectedSize int64, onProgress ProgressFunc) ([]byte, error) {
	return t.DownloadFrom(ctx, []string{url}, expectedSize, onProgress)
}

// DownloadFrom is Download over each address the artifact is published at, in
// order. What one address delivered is resumed from the next: every address
// serves the same bytes, and the caller verifies them whole afterwards.
func (t Transport) DownloadFrom(ctx context.Context, urls []string, expectedSize int64, onProgress ProgressFunc) ([]byte, error) {
	var buf memorySink
	if err := t.fill(ctx, urls, expectedSize, &buf, onProgress); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DownloadFile is DownloadFrom into the file at partial, resuming from what the
// file already holds, so a download cut short by a timeout or a restart picks
// up where it stopped. The caller verifies the file and removes it.
func (t Transport) DownloadFile(ctx context.Context, urls []string, expectedSize int64, partial string, onProgress ProgressFunc) error {
	f, err := os.OpenFile(partial, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrStore, err)
	}
	defer f.Close()
	have, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrStore, err)
	}
	s := &fileSink{f: f, n: have}
	if expectedSize > 0 && have > expectedSize {
		if err := s.Reset(); err != nil {
			return err
		}
	}
	if err := t.fill(ctx, urls, expectedSize, s, onProgress); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("%w: %w", ErrStore, err)
	}
	return nil
}

func (t Transport) fill(ctx context.Context, urls []string, expectedSize int64, s sink, onProgress ProgressFunc) error {
	if expectedSize < 0 || expectedSize > MaxAssetSize {
		return fmt.Errorf("update: invalid expected asset size %d", expectedSize)
	}
	if len(urls) == 0 {
		return fmt.Errorf("update: no address to download from")
	}
	total := expectedSize
	var errs []error
	for _, url := range urls {
		err := t.resume(ctx, url, expectedSize, s, &total, onProgress)
		if err == nil && expectedSize > 0 && s.Size() != expectedSize {
			err = fmt.Errorf("%w: GET %s ended at %d bytes, want %d", ErrSizeMismatch, url, s.Size(), expectedSize)
		}
		if err == nil {
			return nil
		}
		errs = append(errs, err)
		if ctx.Err() != nil || errors.Is(err, ErrStore) {
			break
		}
		if errors.Is(err, ErrSizeMismatch) {
			if rerr := s.Reset(); rerr != nil {
				return errors.Join(append(errs, rerr)...)
			}
		}
	}
	return errors.Join(errs...)
}

// resume retries url until the download completes. Only an attempt that left
// the download no further along counts against Attempts: a slow link that
// stalls but keeps advancing is resumed rather than abandoned, and the loop
// still ends, because every uncounted attempt moves toward a bounded size.
func (t Transport) resume(ctx context.Context, url string, expectedSize int64, s sink, total *int64, onProgress ProgressFunc) error {
	failures := 0
	for attempt := 1; ; attempt++ {
		before := s.Size()
		err := t.downloadInto(ctx, t.clientFor(attempt), url, expectedSize, s, total, onProgress)
		if err == nil {
			return nil
		}
		if s.Size() <= before {
			failures++
		}
		if !Transient(err) || ctx.Err() != nil || failures >= Attempts {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(Backoff(max(failures, 1))):
		}
	}
}

// downloadInto appends url's body to s, dropping the connection once it has
// delivered nothing for StallTimeout.
func (t Transport) downloadInto(ctx context.Context, c *http.Client, url string, expectedSize int64, s sink, total *int64, onProgress ProgressFunc) error {
	var alive func()
	if t.StallTimeout > 0 {
		attemptCtx, cancel := context.WithCancelCause(ctx)
		defer cancel(nil)
		timer := time.AfterFunc(t.StallTimeout, func() { cancel(ErrStalled) })
		defer timer.Stop()
		ctx, alive = attemptCtx, func() { timer.Reset(t.StallTimeout) }
	}
	err := t.receive(ctx, c, url, expectedSize, s, total, onProgress, alive)
	if err != nil && errors.Is(context.Cause(ctx), ErrStalled) {
		return fmt.Errorf("%w: GET %s received nothing for %s", ErrStalled, url, t.StallTimeout)
	}
	return err
}

// receive resumes from s's size via a Range request. A 200 means the server
// ignored Range, so s is reset.
func (t Transport) receive(ctx context.Context, c *http.Client, url string, expectedSize int64, s sink, total *int64, onProgress ProgressFunc, alive func()) error {
	req, err := t.request(ctx, url)
	if err != nil {
		return err
	}
	resumeFrom := s.Size()
	if resumeFrom > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", resumeFrom))
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		if err := s.Reset(); err != nil {
			return err
		}
		if resp.ContentLength > 0 {
			if resp.ContentLength > MaxAssetSize {
				return fmt.Errorf("update: response size %d exceeds maximum %d", resp.ContentLength, MaxAssetSize)
			}
			*total = resp.ContentLength
		}
	case http.StatusPartialContent:
		contentRange := resp.Header.Get("Content-Range")
		if size := TotalFromContentRange(contentRange); size > 0 {
			if size > MaxAssetSize {
				return fmt.Errorf("update: response size %d exceeds maximum %d", size, MaxAssetSize)
			}
			*total = size
		}
		// An intermediary answering a resume from a different offset (CN CDNs and
		// proxies do) would otherwise corrupt the artifact by appending to ours.
		start, ok := RangeStartFromContentRange(contentRange)
		if !ok || start != resumeFrom {
			if err := s.Reset(); err != nil {
				return err
			}
			if start != 0 {
				return &StatusError{URL: url, Status: "206 resumed at " + contentRange, Code: http.StatusPartialContent}
			}
		}
	case http.StatusRequestedRangeNotSatisfiable:
		// A kept partial that is not a prefix of what is served: start over, and
		// let the next attempt do it rather than end the download on it.
		if err := s.Reset(); err != nil {
			return err
		}
		return fmt.Errorf("%w: GET %s: %s", errRangeReset, url, resp.Status)
	default:
		return &StatusError{URL: url, Status: resp.Status, Code: resp.StatusCode}
	}
	have := s.Size()
	if expectedSize > 0 && have > expectedSize {
		return fmt.Errorf("%w: got at least %d want %d", ErrSizeMismatch, have, expectedSize)
	}
	limit := MaxAssetSize - have + 1
	if expectedSize > 0 {
		limit = expectedSize - have + 1
	}
	pr := &progressReader{r: io.LimitReader(resp.Body, limit), received: have, lastEmit: have, total: *total, onProgress: onProgress, alive: alive}
	if _, err = io.Copy(s, pr); err != nil {
		return err
	}
	if expectedSize > 0 && s.Size() > expectedSize {
		return fmt.Errorf("%w: GET %s sent at least %d bytes, want %d", ErrSizeMismatch, url, s.Size(), expectedSize)
	}
	if s.Size() > MaxAssetSize {
		return fmt.Errorf("update: downloaded size exceeds maximum %d", MaxAssetSize)
	}
	return nil
}

// progressReader reports cumulative bytes read, throttled so the event channel
// is not flooded, and tells the stall timer of every read that delivered bytes.
type progressReader struct {
	r          io.Reader
	received   int64
	total      int64
	lastEmit   int64
	onProgress ProgressFunc
	alive      func()
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.received += int64(n)
	if n > 0 && p.alive != nil {
		p.alive()
	}
	// Emit roughly every 256 KiB, and always on the final read (io.EOF).
	if p.onProgress != nil && (p.received-p.lastEmit >= 256<<10 || err == io.EOF) {
		p.lastEmit = p.received
		p.onProgress(p.received, p.total)
	}
	return n, err
}
