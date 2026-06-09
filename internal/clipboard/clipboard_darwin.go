package clipboard

func init() {
	readCmd = []string{"pbpaste"}
	writeCmd = []string{"pbcopy"}
}
