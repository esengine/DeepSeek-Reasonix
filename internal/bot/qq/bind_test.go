package qq

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBindPollDecryptsSecretAndReportsExpiry(t *testing.T) {
	oldBase, oldClient := qqBindBaseURL, bindHTTPClient
	defer func() { qqBindBaseURL, bindHTTPClient = oldBase, oldClient }()
	var key []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/lite/create_bind_task":
			key, _ = base64.StdEncoding.DecodeString(body["key"])
			_, _ = w.Write([]byte(`{"data":{"task_id":"task-1"}}`))
		case "/lite/poll_bind_result":
			if len(key) != 32 {
				t.Errorf("bind key length=%d", len(key))
			}
			secret := encryptBindTestSecret(t, key, "app-secret")
			_, _ = w.Write([]byte(`{"data":{"status":2,"bot_appid":"app-1","bot_encrypt_secret":"` + secret + `","user_openid":"user-1"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	qqBindBaseURL = server.URL
	session, _, err := StartBind(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.Poll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" || result.BotAppID != "app-1" || result.BotSecret != "app-secret" || result.UserOpenID != "user-1" {
		t.Fatalf("result=%+v", result)
	}
}

func encryptBindTestSecret(t *testing.T, key []byte, secret string) string {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, gcm.NonceSize())
	_, _ = rand.Read(nonce)
	blob := append(nonce, gcm.Seal(nil, nonce, []byte(secret), nil)...)
	return base64.StdEncoding.EncodeToString(blob)
}
