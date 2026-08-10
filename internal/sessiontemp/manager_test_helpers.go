package sessiontemp

import "reasonix/internal/filelock"

func tryLockForTest(path string) (func(), error) {
	return filelock.Acquire(nilContext(), path)
}
