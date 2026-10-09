package trustedstate

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestObjectConcurrentStoreHandles(t *testing.T) {
	for _, durable := range []bool{false, true} {
		t.Run(fmt.Sprintf("durable=%v", durable), func(t *testing.T) {
			root := t.TempDir()
			data := bytes.Repeat([]byte("shared object"), 1<<16)
			start := make(chan struct{})
			var wg sync.WaitGroup
			for range 64 {
				wg.Go(func() {
					s := Open(root, nil)
					put := s.PutIndexObject
					if durable {
						put = s.PutObject
					}
					<-start
					d, err := put(data)
					if err != nil {
						t.Errorf("put: %v", err)
						return
					}
					got, err := s.Object(d)
					if err != nil || !bytes.Equal(got, data) {
						t.Errorf("read: bytes match=%v, err=%v", bytes.Equal(got, data), err)
					}
				})
			}
			close(start)
			wg.Wait()
			path, _ := Open(root, nil).objectPath(DigestOf(data))
			files, err := os.ReadDir(filepath.Dir(path))
			if err != nil || len(files) != 1 {
				t.Fatalf("published files = %d, err=%v; want one object without temporary files", len(files), err)
			}
		})
	}
}

func TestObjectStoreHandlesRefuseTamperedWinner(t *testing.T) {
	root := t.TempDir()
	data := []byte("shared object")
	path, _ := Open(root, nil).objectPath(DigestOf(data))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	corrupt := []byte("corrupted object")
	if err := os.WriteFile(path, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for _, durable := range []bool{false, true} {
				s := Open(root, nil)
				put := s.PutIndexObject
				if durable {
					put = s.PutObject
				}
				if _, err := put(data); !errors.Is(err, ErrTampered) {
					t.Errorf("put durable=%v: %v; want ErrTampered", durable, err)
				}
			}
		})
	}
	wg.Wait()
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, corrupt) {
		t.Fatalf("tampered object replaced: %q, %v", got, err)
	}
}
