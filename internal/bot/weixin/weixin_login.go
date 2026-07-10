package weixin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/fileutil"
	fileencoding "reasonix/internal/fileutil/encoding"
)

type savedAccount struct {
	Token   string `json:"token"`
	BaseURL string `json:"base_url"`
	UserID  string `json:"user_id"`
	SavedAt string `json:"saved_at"`
}

type LoginResult struct {
	AccountID string
	Token     string
	BaseURL   string
	UserID    string
}

type LoginSession struct {
	SessionKey string
	QRCode     string
	QRCodeURL  string
	BaseURL    string
	StartedAt  time.Time
}

func weixinAccountDir(root string) string {
	return filepath.Join(root, "weixin", "accounts")
}

func savedAccountPath(accountID string) string {
	root := config.MemoryUserDir()
	if root == "" || accountID == "" {
		return ""
	}
	return filepath.Join(weixinAccountDir(root), accountID+".json")
}

func loadSavedAccount(accountID string) (savedAccount, error) {
	path := savedAccountPath(accountID)
	if path == "" {
		return savedAccount{}, fmt.Errorf("reasonix user config dir is unavailable")
	}
	data, err := fileencoding.ReadFileUTF8(path)
	if err != nil {
		return savedAccount{}, err
	}
	var account savedAccount
	if err := json.Unmarshal(data, &account); err != nil {
		return savedAccount{}, err
	}
	return account, nil
}

func loadAnySavedAccount() (savedAccount, error) {
	root := config.MemoryUserDir()
	if root == "" {
		return savedAccount{}, fmt.Errorf("reasonix user config dir is unavailable")
	}
	entries, err := os.ReadDir(weixinAccountDir(root))
	if err != nil {
		return savedAccount{}, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") || strings.Contains(entry.Name(), "context-tokens") {
			continue
		}
		accountID := strings.TrimSuffix(entry.Name(), ".json")
		account, err := loadSavedAccount(accountID)
		if err == nil && account.Token != "" {
			return account, nil
		}
	}
	return savedAccount{}, fmt.Errorf("no saved weixin account")
}

func HasSavedAccount(accountID string) bool {
	if accountID != "" {
		account, err := loadSavedAccount(accountID)
		return err == nil && account.Token != ""
	}
	account, err := loadSavedAccount("default")
	if err == nil && account.Token != "" {
		return true
	}
	account, err = loadAnySavedAccount()
	return err == nil && account.Token != ""
}

func saveAccount(accountID string, account savedAccount) error {
	path := savedAccountPath(accountID)
	if path == "" {
		return fmt.Errorf("reasonix user config dir is unavailable")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(account, "", "  ")
	if err != nil {
		return err
	}
	// Atomic write: a truncated credentials file silently breaks login.
	return fileutil.AtomicWriteFile(path, data, 0o600)
}

func Login(ctx context.Context, out io.Writer, timeout time.Duration) (*LoginResult, error) {
	if timeout <= 0 {
		timeout = 8 * time.Minute
	}
	session, err := StartLogin(ctx)
	if err != nil {
		return nil, err
	}
	if out != nil {
		fmt.Fprintln(out, "请使用微信扫描以下二维码链接：")
		if session.QRCodeURL != "" {
			fmt.Fprintln(out, session.QRCodeURL)
		} else {
			fmt.Fprintln(out, session.QRCode)
		}
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
		result, status, err := PollLogin(ctx, session)
		if err != nil {
			if out != nil {
				fmt.Fprintf(out, "二维码状态查询失败: %v\n", err)
			}
			continue
		}
		if result != nil {
			return result, nil
		}
		if out != nil {
			switch status {
			case "wait", "", "<nil>":
				fmt.Fprint(out, ".")
			case "scaned":
				fmt.Fprintln(out, "\n已扫码，请在微信里确认...")
			default:
				fmt.Fprintf(out, "\n二维码状态: %s\n", status)
			}
		}
	}
	return nil, fmt.Errorf("weixin login timed out")
}

func StartLogin(ctx context.Context) (*LoginSession, error) {
	qrResp, err := ilinkGET(ctx, defaultWeixinAPI, getBotQRPath+"?bot_type=3")
	if err != nil {
		return nil, fmt.Errorf("fetch qr code: %w", err)
	}
	qrcode := fmt.Sprint(qrResp["qrcode"])
	qrcodeURL := fmt.Sprint(qrResp["qrcode_img_content"])
	if qrcode == "" || qrcode == "<nil>" {
		return nil, fmt.Errorf("weixin qr response missing qrcode")
	}
	if qrcodeURL == "<nil>" {
		qrcodeURL = ""
	}
	return &LoginSession{
		SessionKey: qrcode,
		QRCode:     qrcode,
		QRCodeURL:  qrcodeURL,
		BaseURL:    defaultWeixinAPI,
		StartedAt:  time.Now(),
	}, nil
}

func PollLogin(ctx context.Context, session *LoginSession) (*LoginResult, string, error) {
	if session == nil || session.QRCode == "" {
		return nil, "", fmt.Errorf("weixin login session is missing")
	}
	baseURL := session.BaseURL
	if baseURL == "" {
		baseURL = defaultWeixinAPI
	}
	statusResp, err := ilinkGET(ctx, baseURL, getQRStatusPath+"?qrcode="+session.QRCode)
	if err != nil {
		return nil, "", err
	}
	status := fmt.Sprint(statusResp["status"])
	switch status {
	case "wait", "", "<nil>":
		return nil, status, nil
	case "scaned":
		return nil, status, nil
	case "scaned_but_redirect":
		if host := fmt.Sprint(statusResp["redirect_host"]); host != "" && host != "<nil>" {
			session.BaseURL = "https://" + host
		}
		return nil, status, nil
	case "confirmed":
		accountID := fmt.Sprint(statusResp["ilink_bot_id"])
		token := fmt.Sprint(statusResp["bot_token"])
		userID := fmt.Sprint(statusResp["ilink_user_id"])
		respBaseURL := fmt.Sprint(statusResp["baseurl"])
		if respBaseURL == "" || respBaseURL == "<nil>" {
			respBaseURL = defaultWeixinAPI
		}
		if accountID == "" || accountID == "<nil>" || token == "" || token == "<nil>" {
			return nil, status, fmt.Errorf("weixin qr confirmed but credential payload is incomplete")
		}
		account := savedAccount{
			Token:   token,
			BaseURL: respBaseURL,
			UserID:  userID,
			SavedAt: time.Now().UTC().Format(time.RFC3339),
		}
		if err := saveAccount(accountID, account); err != nil {
			return nil, status, err
		}
		if err := saveAccount("default", account); err != nil {
			return nil, status, err
		}
		return &LoginResult{AccountID: accountID, Token: token, BaseURL: respBaseURL, UserID: userID}, status, nil
	case "expired":
		return nil, status, fmt.Errorf("weixin qr code expired; rerun login")
	default:
		return nil, status, nil
	}
}
