package main

import "fmt"

func installerCommandLine(installer, dir string) string {
	line := fmt.Sprintf(`"%s" /S`, installer)
	if dir != "" {
		line += " /D=" + dir
	}
	return line
}
