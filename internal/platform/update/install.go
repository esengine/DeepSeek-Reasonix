package update

// WindowsHandoff is what the update helper needs to finish an install after
// this process is gone. A value, not five positional strings: every field is a
// path, and swapping two of them compiles.
type WindowsHandoff struct {
	InstallerPath   string // verified artifact in the update cache
	InstallerSHA256 string // digest the helper re-checks before running it
	InstallDir      string // InstallRoot: owns current.json, or the flat dir
	RelaunchPath    string // launcher to start once the install lands
	StagingDir      string // where the verified helper copy is placed
}
