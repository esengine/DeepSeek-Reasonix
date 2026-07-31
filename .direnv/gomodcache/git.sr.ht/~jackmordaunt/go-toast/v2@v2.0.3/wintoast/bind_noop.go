//go:build !windows

package wintoast

var appData AppData

func setAppData(data AppData) error {
	return nil
}

func generateToast(appID string, xml string) error {
	return nil
}

func pushPowershell(xml string) error {
	return nil
}

func pushCOM(appID, xml string) error {
	return nil
}
