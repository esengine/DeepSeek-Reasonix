package registration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	sdkName = "go-sdk"

	defaultFeishuDomain = "https://accounts.feishu.cn"
	defaultLarkDomain   = "https://accounts.larksuite.com"

	endpoint       = "/oauth/v1/app/registration"
	avatarMaxCount = 6

	defaultPollIntervalSeconds = 5
	defaultExpireInSeconds     = 600
)

var waitForInterval = func(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func RegisterApp(ctx context.Context, opts *Options) (*RegisterAppResult, error) {
	if opts == nil {
		return nil, errors.New("registration: options are required")
	}
	if opts.OnQRCode == nil {
		return nil, errors.New("registration: OnQRCode is required")
	}

	domain := opts.Domain
	if domain == "" {
		domain = defaultFeishuDomain
	}
	larkDomain := opts.LarkDomain
	if larkDomain == "" {
		larkDomain = defaultLarkDomain
	}

	beginResp, err := beginRegistration(ctx, domain)
	if err != nil {
		return nil, err
	}

	qrURL, err := buildQRCodeURL(beginResp.VerificationURIComplete, opts.Source, opts.AppPreset)
	if err != nil {
		return nil, err
	}
	opts.OnQRCode(&QRCodeInfo{
		URL:      qrURL,
		ExpireIn: normalizedExpireIn(beginResp.ExpireIn),
	})

	pollCtx, cancel := context.WithTimeout(ctx, time.Duration(normalizedExpireIn(beginResp.ExpireIn))*time.Second)
	defer cancel()

	currentDomain := domain
	interval := normalizedInterval(beginResp.Interval)
	switchedDomain := false
	waitBeforePoll := false

	for {
		if waitBeforePoll {
			if err := waitForInterval(pollCtx, time.Duration(interval)*time.Second); err != nil {
				if errors.Is(err, context.DeadlineExceeded) {
					return nil, &ExpiredError{
						RegisterAppError: &RegisterAppError{
							Code:        "expired_token",
							Description: "registration expired",
						},
					}
				}
				return nil, err
			}
		}
		waitBeforePoll = true

		resp, err := pollRegistration(pollCtx, currentDomain, beginResp.DeviceCode)
		if err != nil {
			return nil, err
		}

		if resp.UserInfo != nil {
			fmt.Printf("tenant brand: %s\n", resp.UserInfo.TenantBrand)
		}

		if resp.UserInfo != nil && resp.UserInfo.TenantBrand == "lark" && !switchedDomain {
			currentDomain = larkDomain
			switchedDomain = true
			emitStatusChange(opts, &StatusChangeInfo{Status: StatusDomainSwitched})
			waitBeforePoll = false
			continue
		}

		if resp.ClientID != "" && resp.ClientSecret != "" {
			return &RegisterAppResult{
				ClientID:     resp.ClientID,
				ClientSecret: resp.ClientSecret,
				UserInfo:     convertUserInfo(resp.UserInfo),
			}, nil
		}

		switch resp.Error {
		case "authorization_pending":
			emitStatusChange(opts, &StatusChangeInfo{Status: StatusPolling})
		case "slow_down":
			interval += 5
			emitStatusChange(opts, &StatusChangeInfo{
				Status:   StatusSlowDown,
				Interval: interval,
			})
		case "access_denied":
			return nil, &AccessDeniedError{
				RegisterAppError: &RegisterAppError{
					Code:        resp.Error,
					Description: resp.ErrorDesc,
				},
			}
		case "expired_token":
			return nil, &ExpiredError{
				RegisterAppError: &RegisterAppError{
					Code:        resp.Error,
					Description: resp.ErrorDesc,
				},
			}
		case "":
			// Keep polling to match the Node SDK behavior when the server
			// responds without an explicit error or final credentials.
		default:
			return nil, &RegisterAppError{
				Code:        resp.Error,
				Description: resp.ErrorDesc,
			}
		}
	}
}

func beginRegistration(ctx context.Context, domain string) (*beginResponse, error) {
	resp := &beginResponse{}
	err := doRegistrationRequest(ctx, domain, url.Values{
		"action":            []string{"begin"},
		"archetype":         []string{"PersonalAgent"},
		"auth_method":       []string{"client_secret"},
		"request_user_info": []string{"open_id"},
	}, resp)
	if err != nil {
		return nil, err
	}
	if resp.DeviceCode == "" {
		return nil, &RegisterAppError{
			Code:        "invalid_response",
			Description: "device_code is empty",
		}
	}
	if resp.VerificationURIComplete == "" {
		return nil, &RegisterAppError{
			Code:        "invalid_response",
			Description: "verification_uri_complete is empty",
		}
	}
	return resp, nil
}

func pollRegistration(ctx context.Context, domain, deviceCode string) (*pollResponse, error) {
	resp := &pollResponse{}
	err := doRegistrationRequest(ctx, domain, url.Values{
		"action":      []string{"poll"},
		"device_code": []string{deviceCode},
	}, resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func doRegistrationRequest(ctx context.Context, domain string, form url.Values, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, buildEndpointURL(domain), strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return &RegisterAppError{
			Code:        "invalid_response",
			Description: "empty response body",
		}
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("registration: decode response failed: %w", err)
	}
	return nil
}

func buildEndpointURL(domain string) string {
	return strings.TrimRight(domain, "/") + endpoint
}

func normalizedInterval(interval int) int {
	if interval <= 0 {
		return defaultPollIntervalSeconds
	}
	return interval
}

func normalizedExpireIn(expireIn int) int {
	if expireIn <= 0 {
		return defaultExpireInSeconds
	}
	return expireIn
}

func buildQRCodeURL(rawURL, source string, preset *AppPreset) (string, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}

	query := parsedURL.Query()
	query.Set("from", "sdk")
	query.Set("tp", "sdk")
	if source == "" {
		query.Set("source", sdkName)
	} else {
		query.Set("source", sdkName+"/"+source)
	}
	if err := applyAppPreset(query, preset); err != nil {
		return "", err
	}
	parsedURL.RawQuery = query.Encode()
	return parsedURL.String(), nil
}

func applyAppPreset(query url.Values, preset *AppPreset) error {
	if preset == nil {
		return nil
	}
	if len(preset.Avatar) > avatarMaxCount {
		return fmt.Errorf("registration: AppPreset.Avatar supports at most %d URLs, got %d", avatarMaxCount, len(preset.Avatar))
	}
	for idx, avatar := range preset.Avatar {
		if avatar == "" {
			return fmt.Errorf("registration: AppPreset.Avatar[%d] must be a non-empty string", idx)
		}
		query.Add("avatar", avatar)
	}
	if preset.Name != "" {
		query.Set("name", preset.Name)
	}
	if preset.Desc != "" {
		query.Set("desc", preset.Desc)
	}
	return nil
}

func emitStatusChange(opts *Options, info *StatusChangeInfo) {
	if opts.OnStatusChange != nil {
		opts.OnStatusChange(info)
	}
}

func convertUserInfo(info *userInfo) *UserInfo {
	if info == nil {
		return nil
	}
	return &UserInfo{
		OpenID:      info.OpenID,
		TenantBrand: info.TenantBrand,
	}
}
