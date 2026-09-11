//go:build !windows

package desktopinstance

func PrepareInstall(root, home string, interactive bool) (func(), error) { return func() {}, nil }
func LaunchAndVerify(root, home string, interactive bool, start func() error, args ...string) error {
	return start()
}
func Notify(err error)                           {}
func CheckInstallVacant(root, home string) error { return nil }
