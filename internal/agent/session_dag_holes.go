package agent

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
)

// resumePastTornLine continues a replay that stopped at a line it could not
// decode. When complete entries follow the bad line it is skipped as a hole,
// so a tail another writer appended after a crash-torn line (or an unlocked
// shutdown append behind one) stays readable; a bad region that runs to the
// end of the file is the torn tail repairSessionDAGTail already handles.
func (st *sessionDAGState) resumePastTornLine(ctx context.Context, limits sessionReplayLimits) error {
	badStart := st.lastGoodEnd
	if badStart > 0 {
		badStart++
	}
	next, ok, err := nextLineStart(st.path, badStart, limits.maxBytes)
	if err != nil {
		return err
	}
	if !ok {
		st.damaged = true
		return nil
	}
	records := st.records
	if err := st.replayFrom(ctx, next, limits); err != nil {
		return err
	}
	if st.records == records && st.damaged {
		return nil
	}
	st.holes++
	slog.Warn("session: skipped an unreadable line inside the event log", "path", st.path, "from", badStart, "to", next)
	return nil
}

// nextLineStart returns the offset just past the first newline at or after
// from; ok is false when the rest of the file has none.
func nextLineStart(path string, from, maxBytes int64) (int64, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false, err
	}
	defer f.Close()
	if _, err := f.Seek(from, io.SeekStart); err != nil {
		return 0, false, err
	}
	r := bufio.NewReader(io.LimitReader(f, maxBytes+1-from))
	pos := from
	for {
		chunk, err := r.ReadSlice('\n')
		pos += int64(len(chunk))
		switch {
		case err == nil:
			return pos, true, nil
		case errors.Is(err, bufio.ErrBufferFull):
		case errors.Is(err, io.EOF):
			return 0, false, nil
		default:
			return 0, false, err
		}
	}
}
