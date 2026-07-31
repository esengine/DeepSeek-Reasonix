package doublestar

import (
	"log"
	"syscall"
)

func mkdirpHidden(parts ...string) string {
	dir := mkdirp(parts...)
	return makeHidden(dir)
}

func touchHidden(parts ...string) string {
	file := touch(parts...)
	return makeHidden(file)
}

func makeHidden(path string) string {
	pointer, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		log.Fatalf("Could not make %v hidden: %v\n", path, err)
		return path
	}

	attributes, err := syscall.GetFileAttributes(pointer)
	if err != nil {
		log.Fatalf("Could not make %v hidden: %v\n", path, err)
		return path
	}

	if attributes&syscall.FILE_ATTRIBUTE_HIDDEN == 0 {
		err = syscall.SetFileAttributes(pointer, attributes|syscall.FILE_ATTRIBUTE_HIDDEN)
		if err != nil {
			log.Fatalf("Could not make %v hidden: %v\n", path, err)
		}
	}

	return path
}
