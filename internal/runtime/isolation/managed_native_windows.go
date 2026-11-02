package isolation

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func candidateDirectoryID(file *os.File) (string, error) {
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errUnavailable
	}
	var stat windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &stat); err != nil {
		return "", err
	}
	return fmt.Sprintf("windows:%d:%d:%d", stat.VolumeSerialNumber, stat.FileIndexHigh, stat.FileIndexLow), nil
}
