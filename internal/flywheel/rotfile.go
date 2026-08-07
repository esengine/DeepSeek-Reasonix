package flywheel

import (
	"os"
	"path/filepath"
)

// rotFile is a small append-only daily file wrapper. It deliberately lives in
// this package (not os) so the flywheel Writer stays self-contained.
type rotFile struct {
	f *os.File
}

func openRotFile(dir, day string) (*rotFile, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, day+".jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &rotFile{f: f}, nil
}

func (r *rotFile) writeLine(buf []byte) error {
	if r == nil || r.f == nil {
		return nil
	}
	_, err := r.f.Write(append(buf, '\n'))
	return err
}

func (r *rotFile) Close() error {
	if r == nil || r.f == nil {
		return nil
	}
	err := r.f.Close()
	r.f = nil
	return err
}
