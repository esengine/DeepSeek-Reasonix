//go:build e2e
// +build e2e

package registration

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

const defaultE2ETimeout = 10 * time.Minute

func TestRegisterAppE2EWithAppPreset(t *testing.T) {
	timeout := envDuration("REGISTRATION_E2E_TIMEOUT", defaultE2ETimeout)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	opts := &Options{
		Source:     envString("REGISTRATION_E2E_SOURCE", "registration-app-preset-e2e"),
		Domain:     os.Getenv("REGISTRATION_E2E_DOMAIN"),
		LarkDomain: os.Getenv("REGISTRATION_E2E_LARK_DOMAIN"),
		AppPreset: &AppPreset{
			Avatar: envCSV("REGISTRATION_E2E_AVATARS", []string{
				"https://s1-imfile.feishucdn.com/static-resource/v1/v3_00cj_d6bebede-c56b-40a2-b767-8e9da07f3b3g",
				"https://s1-imfile.feishucdn.com/static-resource/v1/v2_bc5d2075-fcbd-41f8-bfe3-5a5ecbf0f7dg",
			}),
			Name: envString("REGISTRATION_E2E_NAME", "{user}的应用"),
			Desc: envString("REGISTRATION_E2E_DESC", "由 Go SDK E2E 自动生成"),
		},
		OnQRCode: func(info *QRCodeInfo) {
			t.Logf("open or scan this url before it expires in %d seconds:\n%s", info.ExpireIn, info.URL)
			if os.Getenv("REGISTRATION_E2E_OPEN_BROWSER") == "1" {
				openBrowser(t, info.URL)
			}
		},
		OnStatusChange: func(info *StatusChangeInfo) {
			if info.Interval > 0 {
				t.Logf("registration status: %s, next poll after %d seconds", info.Status, info.Interval)
				return
			}
			t.Logf("registration status: %s", info.Status)
		},
	}

	result, err := RegisterApp(ctx, opts)
	if err != nil {
		t.Fatalf("register app e2e failed: %v", err)
	}
	if result.ClientID == "" {
		t.Fatal("expected client id")
	}
	t.Logf("registration succeeded: client_id=%s", result.ClientID)
	if result.UserInfo != nil {
		t.Logf("user_info: open_id=%s tenant_brand=%s", result.UserInfo.OpenID, result.UserInfo.TenantBrand)
	}
}

func envString(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envCSV(key string, fallback []string) []string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			values = append(values, trimmed)
		}
	}
	return values
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return duration
}

func openBrowser(t *testing.T, rawURL string) {
	t.Helper()

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", rawURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	default:
		cmd = exec.Command("xdg-open", rawURL)
	}
	if err := cmd.Start(); err != nil {
		t.Logf("open browser failed: %v", err)
	}
}

func ExampleRegisterApp_e2eCommand() {
	fmt.Println("go test -tags=e2e ./scene/registration -run TestRegisterAppE2EWithAppPreset -v -count=1")
	// Output:
	// go test -tags=e2e ./scene/registration -run TestRegisterAppE2EWithAppPreset -v -count=1
}
