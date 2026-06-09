package clipboard

func init() {
	readCmd = []string{"powershell", "-NoProfile", "-Command", "Get-Clipboard"}
	writeCmd = []string{"powershell", "-NoProfile", "-Command", "Set-Clipboard"}
}
