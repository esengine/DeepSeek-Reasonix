package qq

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var qqBindBaseURL = "https://q.qq.com"

var bindHTTPClient = &http.Client{Timeout: 15 * time.Second}

type BindSession struct {
	TaskID    string
	Key       []byte
	StartedAt time.Time
	ExpireAt  time.Time
}

type BindResult struct {
	Status     string
	TaskID     string
	BotAppID   string
	BotSecret  string
	UserOpenID string
}

func StartBind(ctx context.Context) (*BindSession, string, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, "", err
	}
	encoded := base64.StdEncoding.EncodeToString(key)
	data, err := bindJSON(ctx, http.MethodPost, qqBindBaseURL+"/lite/create_bind_task", map[string]string{"key": encoded})
	if err != nil {
		return nil, "", err
	}
	var response struct {
		Data struct {
			TaskID string `json:"task_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, "", err
	}
	if strings.TrimSpace(response.Data.TaskID) == "" {
		return nil, "", fmt.Errorf("qq bind response has no task_id")
	}
	now := time.Now()
	session := &BindSession{TaskID: response.Data.TaskID, Key: key, StartedAt: now, ExpireAt: now.Add(5 * time.Minute)}
	return session, "https://q.qq.com/qqbot/openclaw/connect.html?source=reasonix&_wv=2&task_id=" + response.Data.TaskID, nil
}

func (s *BindSession) Poll(ctx context.Context) (BindResult, error) {
	if s == nil || len(s.Key) != 32 || strings.TrimSpace(s.TaskID) == "" {
		return BindResult{}, fmt.Errorf("qq bind session is invalid")
	}
	data, err := bindJSON(ctx, http.MethodPost, qqBindBaseURL+"/lite/poll_bind_result", map[string]string{"task_id": s.TaskID})
	if err != nil {
		return BindResult{}, err
	}
	var response struct {
		Data struct {
			Status           int    `json:"status"`
			BotAppID         string `json:"bot_appid"`
			BotEncryptSecret string `json:"bot_encrypt_secret"`
			UserOpenID       string `json:"user_openid"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return BindResult{}, err
	}
	result := BindResult{TaskID: s.TaskID, BotAppID: response.Data.BotAppID, UserOpenID: response.Data.UserOpenID}
	switch response.Data.Status {
	case 0:
		result.Status = "none"
	case 1:
		result.Status = "pending"
	case 2:
		result.Status = "completed"
		secret, err := decryptBindSecret(s.Key, response.Data.BotEncryptSecret)
		if err != nil {
			return BindResult{}, err
		}
		result.BotSecret = secret
	case 3:
		result.Status = "expired"
	default:
		result.Status = "unknown"
	}
	return result, nil
}

func decryptBindSecret(key []byte, encoded string) (string, error) {
	blob, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return "", err
	}
	if len(blob) < 12+16+1 {
		return "", fmt.Errorf("qq bind secret is too short")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce, ciphertext := blob[:12], blob[12:]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt qq bind secret: %w", err)
	}
	return string(plain), nil
}

func bindJSON(ctx context.Context, method, endpoint string, body any) ([]byte, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := bindHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	response, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("qq bind api error %d: %s", resp.StatusCode, string(response))
	}
	return response, nil
}
