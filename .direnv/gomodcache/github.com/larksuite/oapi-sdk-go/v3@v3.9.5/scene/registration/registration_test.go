package registration

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRegisterAppRequiresOnQRCode(t *testing.T) {
	_, err := RegisterApp(context.Background(), &Options{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "registration: OnQRCode is required" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRegisterAppSuccess(t *testing.T) {
	restoreWait := stubWaitForInterval()
	defer restoreWait()

	pollCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form failed: %v", err)
		}
		switch r.Form.Get("action") {
		case "begin":
			writeJSON(w, `{"device_code":"device-1","verification_uri_complete":"https://qr.example.com/scan?foo=bar","interval":0,"expire_in":60}`)
		case "poll":
			pollCount++
			if pollCount == 1 {
				writeJSON(w, `{"error":"authorization_pending","error_description":"pending"}`)
				return
			}
			writeJSON(w, `{"client_id":"cli_a","client_secret":"sec_a","user_info":{"open_id":"ou_x","tenant_brand":"feishu"}}`)
		default:
			t.Fatalf("unexpected action: %s", r.Form.Get("action"))
		}
	}))
	defer server.Close()

	var qrInfo *QRCodeInfo
	statuses := make([]string, 0, 1)
	result, err := RegisterApp(context.Background(), &Options{
		Domain: server.URL,
		Source: "my-cli",
		OnQRCode: func(info *QRCodeInfo) {
			qrInfo = info
		},
		OnStatusChange: func(info *StatusChangeInfo) {
			statuses = append(statuses, info.Status)
		},
	})
	if err != nil {
		t.Fatalf("register app failed: %v", err)
	}
	if result.ClientID != "cli_a" || result.ClientSecret != "sec_a" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.UserInfo == nil || result.UserInfo.OpenID != "ou_x" || result.UserInfo.TenantBrand != "feishu" {
		t.Fatalf("unexpected user info: %#v", result.UserInfo)
	}
	if qrInfo == nil {
		t.Fatal("expected qr info")
	}
	if qrInfo.ExpireIn != 60 {
		t.Fatalf("unexpected expire_in: %d", qrInfo.ExpireIn)
	}
	parsedQR, err := url.Parse(qrInfo.URL)
	if err != nil {
		t.Fatalf("parse qr url failed: %v", err)
	}
	query := parsedQR.Query()
	if query.Get("foo") != "bar" {
		t.Fatalf("unexpected original query: %s", query.Encode())
	}
	if query.Get("from") != "sdk" || query.Get("tp") != "sdk" || query.Get("source") != "go-sdk/my-cli" {
		t.Fatalf("unexpected qr query: %s", query.Encode())
	}
	if len(statuses) != 1 || statuses[0] != StatusPolling {
		t.Fatalf("unexpected statuses: %#v", statuses)
	}
}

func TestRegisterAppOmitsAppPresetWhenNil(t *testing.T) {
	qrURL := captureQRURL(t, Options{})
	query := qrURL.Query()

	if _, ok := query["avatar"]; ok {
		t.Fatalf("unexpected avatar param: %s", qrURL.String())
	}
	if _, ok := query["name"]; ok {
		t.Fatalf("unexpected name param: %s", qrURL.String())
	}
	if _, ok := query["desc"]; ok {
		t.Fatalf("unexpected desc param: %s", qrURL.String())
	}
	if query.Get("from") != "sdk" || query.Get("source") != "go-sdk" || query.Get("tp") != "sdk" {
		t.Fatalf("unexpected qr query: %s", query.Encode())
	}
}

func TestRegisterAppAppPresetSingleAvatar(t *testing.T) {
	qrURL := captureQRURL(t, Options{
		AppPreset: &AppPreset{
			Avatar: []string{"https://example.com/a.png"},
		},
	})

	if !reflect.DeepEqual(qrURL.Query()["avatar"], []string{"https://example.com/a.png"}) {
		t.Fatalf("unexpected avatar params: %#v", qrURL.Query()["avatar"])
	}
}

func TestRegisterAppAppPresetMultipleAvatarsPreserveOrder(t *testing.T) {
	avatars := []string{
		"https://example.com/a.png",
		"https://example.com/b.webp",
		"https://example.com/c.gif",
	}
	qrURL := captureQRURL(t, Options{
		AppPreset: &AppPreset{
			Avatar: avatars,
		},
	})

	if !reflect.DeepEqual(qrURL.Query()["avatar"], avatars) {
		t.Fatalf("unexpected avatar params: %#v", qrURL.Query()["avatar"])
	}
}

func TestRegisterAppAppPresetAcceptsExactlySixAvatars(t *testing.T) {
	avatars := []string{
		"https://example.com/0.png",
		"https://example.com/1.png",
		"https://example.com/2.png",
		"https://example.com/3.png",
		"https://example.com/4.png",
		"https://example.com/5.png",
	}
	qrURL := captureQRURL(t, Options{
		AppPreset: &AppPreset{
			Avatar: avatars,
		},
	})

	if !reflect.DeepEqual(qrURL.Query()["avatar"], avatars) {
		t.Fatalf("unexpected avatar params: %#v", qrURL.Query()["avatar"])
	}
}

func TestRegisterAppAppPresetRejectsMoreThanSixAvatars(t *testing.T) {
	expectAppPresetReject(t, &AppPreset{
		Avatar: []string{
			"https://example.com/0.png",
			"https://example.com/1.png",
			"https://example.com/2.png",
			"https://example.com/3.png",
			"https://example.com/4.png",
			"https://example.com/5.png",
			"https://example.com/6.png",
		},
	}, "AppPreset.Avatar supports at most 6 URLs, got 7")
}

func TestRegisterAppAppPresetRejectsEmptyAvatarEntry(t *testing.T) {
	expectAppPresetReject(t, &AppPreset{
		Avatar: []string{"https://example.com/a.png", ""},
	}, "AppPreset.Avatar[1] must be a non-empty string")
}

func TestRegisterAppAppPresetEncodesNameWithUserPlaceholder(t *testing.T) {
	qrURL := captureQRURL(t, Options{
		AppPreset: &AppPreset{
			Name: "{user}的应用",
		},
	})

	if got := qrURL.Query().Get("name"); got != "{user}的应用" {
		t.Fatalf("unexpected name: %s", got)
	}
	if !strings.Contains(qrURL.String(), "name=%7Buser%7D%E7%9A%84%E5%BA%94%E7%94%A8") {
		t.Fatalf("expected encoded name in raw qr url: %s", qrURL.String())
	}
}

func TestRegisterAppAppPresetEncodesDesc(t *testing.T) {
	qrURL := captureQRURL(t, Options{
		AppPreset: &AppPreset{
			Desc: "由业务平台自动生成",
		},
	})

	if got := qrURL.Query().Get("desc"); got != "由业务平台自动生成" {
		t.Fatalf("unexpected desc: %s", got)
	}
}

func TestRegisterAppAppPresetEmitsAllFieldsTogether(t *testing.T) {
	qrURL := captureQRURL(t, Options{
		AppPreset: &AppPreset{
			Avatar: []string{"https://example.com/a.png", "https://example.com/b.png"},
			Name:   "MyApp",
			Desc:   "demo",
		},
	})
	query := qrURL.Query()

	if !reflect.DeepEqual(query["avatar"], []string{"https://example.com/a.png", "https://example.com/b.png"}) {
		t.Fatalf("unexpected avatar params: %#v", query["avatar"])
	}
	if query.Get("name") != "MyApp" {
		t.Fatalf("unexpected name: %s", query.Get("name"))
	}
	if query.Get("desc") != "demo" {
		t.Fatalf("unexpected desc: %s", query.Get("desc"))
	}
}

func TestRegisterAppAppPresetDoesNotInterfereWithSource(t *testing.T) {
	qrURL := captureQRURL(t, Options{
		Source: "lark-cli",
		AppPreset: &AppPreset{
			Name: "X",
		},
	})
	query := qrURL.Query()

	if query.Get("source") != "go-sdk/lark-cli" {
		t.Fatalf("unexpected source: %s", query.Get("source"))
	}
	if query.Get("name") != "X" {
		t.Fatalf("unexpected name: %s", query.Get("name"))
	}
}

func TestRegisterAppMockFlowForLocalRun(t *testing.T) {
	restoreWait := stubWaitForInterval()
	defer restoreWait()

	beginCalled := 0
	pollCalled := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form failed: %v", err)
		}

		action := r.Form.Get("action")
		switch action {
		case "begin":
			beginCalled++
			writeJSON(w, `{
				"device_code":"mock-device-code",
				"verification_uri_complete":"https://mock-qr.example.com/activate?ticket=abc123",
				"interval":0,
				"expire_in":120
			}`)
		case "poll":
			pollCalled++
			if r.Form.Get("device_code") != "mock-device-code" {
				t.Fatalf("unexpected device_code: %s", r.Form.Get("device_code"))
			}
			if pollCalled == 1 {
				writeJSON(w, `{
					"error":"authorization_pending",
					"error_description":"waiting for user scan"
				}`)
				return
			}
			writeJSON(w, `{
				"client_id":"cli_mock_local",
				"client_secret":"sec_mock_local",
				"user_info":{
					"open_id":"ou_mock_user",
					"tenant_brand":"feishu"
				}
			}`)
		default:
			t.Fatalf("unexpected action: %s", action)
		}
	}))
	defer server.Close()

	var qrInfo *QRCodeInfo
	statuses := make([]string, 0, 2)
	result, err := RegisterApp(context.Background(), &Options{
		Domain: server.URL,
		Source: "local-test",
		OnQRCode: func(info *QRCodeInfo) {
			qrInfo = info
			t.Logf("mock qr generated: url=%s expire_in=%d", info.URL, info.ExpireIn)
		},
		OnStatusChange: func(info *StatusChangeInfo) {
			statuses = append(statuses, info.Status)
			t.Logf("mock status changed: status=%s interval=%d", info.Status, info.Interval)
		},
	})
	if err != nil {
		t.Fatalf("register app failed: %v", err)
	}

	if beginCalled != 1 {
		t.Fatalf("unexpected begin count: %d", beginCalled)
	}
	if pollCalled != 2 {
		t.Fatalf("unexpected poll count: %d", pollCalled)
	}
	if qrInfo == nil {
		t.Fatal("expected qr info")
	}

	parsedQR, err := url.Parse(qrInfo.URL)
	if err != nil {
		t.Fatalf("parse qr url failed: %v", err)
	}
	query := parsedQR.Query()
	if query.Get("ticket") != "abc123" {
		t.Fatalf("unexpected qr query: %s", query.Encode())
	}
	if query.Get("from") != "sdk" || query.Get("source") != "go-sdk/local-test" || query.Get("tp") != "sdk" {
		t.Fatalf("unexpected qr query params: %s", query.Encode())
	}

	if result.ClientID != "cli_mock_local" || result.ClientSecret != "sec_mock_local" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.UserInfo == nil || result.UserInfo.OpenID != "ou_mock_user" {
		t.Fatalf("unexpected user info: %#v", result.UserInfo)
	}
	if len(statuses) != 1 || statuses[0] != StatusPolling {
		t.Fatalf("unexpected statuses: %#v", statuses)
	}

	t.Logf("mock registration finished: client_id=%s poll_count=%d", result.ClientID, pollCalled)
}

func TestRegisterAppPollsImmediatelyThenUsesReturnedInterval(t *testing.T) {
	getIntervals, restoreWait := stubWaitRecorder()
	defer restoreWait()

	pollCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form failed: %v", err)
		}
		switch r.Form.Get("action") {
		case "begin":
			writeJSON(w, `{"device_code":"device-immediate","verification_uri_complete":"https://qr.example.com/scan","interval":7,"expire_in":60}`)
		case "poll":
			pollCount++
			if pollCount == 1 {
				writeJSON(w, `{"error":"authorization_pending","error_description":"pending"}`)
				return
			}
			writeJSON(w, `{"client_id":"cli_immediate","client_secret":"sec_immediate"}`)
		default:
			t.Fatalf("unexpected action: %s", r.Form.Get("action"))
		}
	}))
	defer server.Close()

	result, err := RegisterApp(context.Background(), &Options{
		Domain:   server.URL,
		OnQRCode: func(info *QRCodeInfo) {},
	})
	if err != nil {
		t.Fatalf("register app failed: %v", err)
	}
	if result.ClientID != "cli_immediate" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if pollCount != 2 {
		t.Fatalf("unexpected poll count: %d", pollCount)
	}
	intervals := getIntervals()
	if !reflect.DeepEqual(intervals, []time.Duration{7 * time.Second}) {
		t.Fatalf("unexpected wait intervals: %#v", intervals)
	}
}

func TestRegisterAppUsesDefaultIntervalAndExpireIn(t *testing.T) {
	getIntervals, restoreWait := stubWaitRecorder()
	defer restoreWait()

	pollCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form failed: %v", err)
		}
		switch r.Form.Get("action") {
		case "begin":
			writeJSON(w, `{"device_code":"device-default","verification_uri_complete":"https://qr.example.com/scan","interval":0,"expire_in":0}`)
		case "poll":
			pollCount++
			writeJSON(w, `{"client_id":"cli_default","client_secret":"sec_default"}`)
		default:
			t.Fatalf("unexpected action: %s", r.Form.Get("action"))
		}
	}))
	defer server.Close()

	var qrInfo *QRCodeInfo
	result, err := RegisterApp(context.Background(), &Options{
		Domain: server.URL,
		OnQRCode: func(info *QRCodeInfo) {
			qrInfo = info
		},
	})
	if err != nil {
		t.Fatalf("register app failed: %v", err)
	}
	if result.ClientID != "cli_default" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if qrInfo == nil || qrInfo.ExpireIn != defaultExpireInSeconds {
		t.Fatalf("unexpected qr info: %#v", qrInfo)
	}
	intervals := getIntervals()
	if len(intervals) != 0 {
		t.Fatalf("unexpected wait intervals: %#v", intervals)
	}
}

func TestRegisterAppKeepsPollingOnEmptyPollResponse(t *testing.T) {
	getIntervals, restoreWait := stubWaitRecorder()
	defer restoreWait()

	pollCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form failed: %v", err)
		}
		switch r.Form.Get("action") {
		case "begin":
			writeJSON(w, `{"device_code":"device-empty","verification_uri_complete":"https://qr.example.com/scan","interval":3,"expire_in":60}`)
		case "poll":
			pollCount++
			if pollCount == 1 {
				writeJSON(w, `{}`)
				return
			}
			writeJSON(w, `{"client_id":"cli_after_empty","client_secret":"sec_after_empty"}`)
		default:
			t.Fatalf("unexpected action: %s", r.Form.Get("action"))
		}
	}))
	defer server.Close()

	result, err := RegisterApp(context.Background(), &Options{
		Domain:   server.URL,
		OnQRCode: func(info *QRCodeInfo) {},
	})
	if err != nil {
		t.Fatalf("register app failed: %v", err)
	}
	if result.ClientID != "cli_after_empty" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if pollCount != 2 {
		t.Fatalf("unexpected poll count: %d", pollCount)
	}
	intervals := getIntervals()
	if !reflect.DeepEqual(intervals, []time.Duration{3 * time.Second}) {
		t.Fatalf("unexpected wait intervals: %#v", intervals)
	}
}

func TestRegisterAppSwitchesToLarkDomain(t *testing.T) {
	restoreWait := stubWaitForInterval()
	defer restoreWait()

	larkPollCount := 0
	larkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form failed: %v", err)
		}
		larkPollCount++
		writeJSON(w, `{"client_id":"cli_lark","client_secret":"sec_lark","user_info":{"open_id":"ou_lark","tenant_brand":"lark"}}`)
	}))
	defer larkServer.Close()

	feishuPollCount := 0
	feishuServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form failed: %v", err)
		}
		switch r.Form.Get("action") {
		case "begin":
			writeJSON(w, `{"device_code":"device-2","verification_uri_complete":"https://qr.example.com/scan","interval":0,"expire_in":60}`)
		case "poll":
			feishuPollCount++
			writeJSON(w, `{"user_info":{"tenant_brand":"lark"}}`)
		default:
			t.Fatalf("unexpected action: %s", r.Form.Get("action"))
		}
	}))
	defer feishuServer.Close()

	statuses := make([]string, 0, 1)
	result, err := RegisterApp(context.Background(), &Options{
		Domain:     feishuServer.URL,
		LarkDomain: larkServer.URL,
		OnQRCode:   func(info *QRCodeInfo) {},
		OnStatusChange: func(info *StatusChangeInfo) {
			statuses = append(statuses, info.Status)
		},
	})
	if err != nil {
		t.Fatalf("register app failed: %v", err)
	}
	if result.ClientID != "cli_lark" || result.ClientSecret != "sec_lark" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if feishuPollCount != 1 {
		t.Fatalf("unexpected feishu poll count: %d", feishuPollCount)
	}
	if larkPollCount != 1 {
		t.Fatalf("unexpected lark poll count: %d", larkPollCount)
	}
	if len(statuses) != 1 || statuses[0] != StatusDomainSwitched {
		t.Fatalf("unexpected statuses: %#v", statuses)
	}
}

func TestRegisterAppSlowDown(t *testing.T) {
	restoreWait := stubWaitForInterval()
	defer restoreWait()

	pollCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form failed: %v", err)
		}
		switch r.Form.Get("action") {
		case "begin":
			writeJSON(w, `{"device_code":"device-3","verification_uri_complete":"https://qr.example.com/scan","interval":0,"expire_in":60}`)
		case "poll":
			pollCount++
			if pollCount == 1 {
				writeJSON(w, `{"error":"slow_down","error_description":"slow down"}`)
				return
			}
			writeJSON(w, `{"client_id":"cli_b","client_secret":"sec_b"}`)
		default:
			t.Fatalf("unexpected action: %s", r.Form.Get("action"))
		}
	}))
	defer server.Close()

	var slowDownInfo *StatusChangeInfo
	result, err := RegisterApp(context.Background(), &Options{
		Domain:   server.URL,
		OnQRCode: func(info *QRCodeInfo) {},
		OnStatusChange: func(info *StatusChangeInfo) {
			if info.Status == StatusSlowDown {
				slowDownInfo = info
			}
		},
	})
	if err != nil {
		t.Fatalf("register app failed: %v", err)
	}
	if result.ClientID != "cli_b" || result.ClientSecret != "sec_b" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if slowDownInfo == nil {
		t.Fatal("expected slow_down status")
	}
	if slowDownInfo.Interval != 10 {
		t.Fatalf("unexpected interval: %d", slowDownInfo.Interval)
	}
}

func TestRegisterAppAccessDenied(t *testing.T) {
	restoreWait := stubWaitForInterval()
	defer restoreWait()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form failed: %v", err)
		}
		switch r.Form.Get("action") {
		case "begin":
			writeJSON(w, `{"device_code":"device-4","verification_uri_complete":"https://qr.example.com/scan","interval":0,"expire_in":60}`)
		case "poll":
			writeJSON(w, `{"error":"access_denied","error_description":"user denied"}`)
		default:
			t.Fatalf("unexpected action: %s", r.Form.Get("action"))
		}
	}))
	defer server.Close()

	_, err := RegisterApp(context.Background(), &Options{
		Domain:   server.URL,
		OnQRCode: func(info *QRCodeInfo) {},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var accessDenied *AccessDeniedError
	if !errors.As(err, &accessDenied) {
		t.Fatalf("unexpected error type: %#v", err)
	}
	if accessDenied.Code != "access_denied" || accessDenied.Description != "user denied" {
		t.Fatalf("unexpected error: %#v", accessDenied)
	}
}

func TestRegisterAppExpiredToken(t *testing.T) {
	restoreWait := stubWaitForInterval()
	defer restoreWait()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form failed: %v", err)
		}
		switch r.Form.Get("action") {
		case "begin":
			writeJSON(w, `{"device_code":"device-5","verification_uri_complete":"https://qr.example.com/scan","interval":0,"expire_in":60}`)
		case "poll":
			writeJSON(w, `{"error":"expired_token","error_description":"qr expired"}`)
		default:
			t.Fatalf("unexpected action: %s", r.Form.Get("action"))
		}
	}))
	defer server.Close()

	_, err := RegisterApp(context.Background(), &Options{
		Domain:   server.URL,
		OnQRCode: func(info *QRCodeInfo) {},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var expired *ExpiredError
	if !errors.As(err, &expired) {
		t.Fatalf("unexpected error type: %#v", err)
	}
	if expired.Code != "expired_token" || expired.Description != "qr expired" {
		t.Fatalf("unexpected error: %#v", expired)
	}
}

func stubWaitForInterval() func() {
	original := waitForInterval
	waitForInterval = func(ctx context.Context, intervalDuration time.Duration) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return nil
	}
	return func() {
		waitForInterval = original
	}
}

func stubWaitRecorder() (func() []time.Duration, func()) {
	original := waitForInterval
	intervals := make([]time.Duration, 0, 4)
	waitForInterval = func(ctx context.Context, intervalDuration time.Duration) error {
		intervals = append(intervals, intervalDuration)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return nil
	}
	return func() []time.Duration {
			copied := make([]time.Duration, len(intervals))
			copy(copied, intervals)
			return copied
		}, func() {
			waitForInterval = original
		}
}

func captureQRURL(t *testing.T, opts Options) *url.URL {
	t.Helper()

	restoreWait := stubWaitForInterval()
	defer restoreWait()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form failed: %v", err)
		}
		switch r.Form.Get("action") {
		case "begin":
			writeJSON(w, `{"device_code":"dev-1","verification_uri_complete":"https://accounts.feishu.cn/page/launcher","verification_uri":"https://accounts.feishu.cn/page/launcher","user_code":"uc-1","interval":0,"expire_in":600}`)
		case "poll":
			writeJSON(w, `{"client_id":"cli_test","client_secret":"sec_test"}`)
		default:
			t.Fatalf("unexpected action: %s", r.Form.Get("action"))
		}
	}))
	defer server.Close()

	var captured string
	opts.Domain = server.URL
	if opts.OnQRCode == nil {
		opts.OnQRCode = func(info *QRCodeInfo) {
			captured = info.URL
		}
	}

	if _, err := RegisterApp(context.Background(), &opts); err != nil {
		t.Fatalf("register app failed: %v", err)
	}
	if captured == "" {
		t.Fatal("expected qr url")
	}
	parsed, err := url.Parse(captured)
	if err != nil {
		t.Fatalf("parse qr url failed: %v", err)
	}
	return parsed
}

func expectAppPresetReject(t *testing.T, preset *AppPreset, wantErr string) {
	t.Helper()

	restoreWait := stubWaitForInterval()
	defer restoreWait()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form failed: %v", err)
		}
		switch r.Form.Get("action") {
		case "begin":
			writeJSON(w, `{"device_code":"dev-1","verification_uri_complete":"https://accounts.feishu.cn/page/launcher","interval":0,"expire_in":600}`)
		case "poll":
			t.Fatal("poll should not be called when preset validation fails")
		default:
			t.Fatalf("unexpected action: %s", r.Form.Get("action"))
		}
	}))
	defer server.Close()

	calledQRCode := false
	_, err := RegisterApp(context.Background(), &Options{
		Domain:    server.URL,
		AppPreset: preset,
		OnQRCode: func(info *QRCodeInfo) {
			calledQRCode = true
		},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("unexpected error: %v", err)
	}
	if calledQRCode {
		t.Fatal("OnQRCode should not be called")
	}
}

func writeJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(body))
}
