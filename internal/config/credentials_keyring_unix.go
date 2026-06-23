//go:build (dragonfly && cgo) || (freebsd && cgo) || linux || netbsd || openbsd

package config

import (
	"strings"

	ss "github.com/zalando/go-keyring/secret_service"
)

func legacyKeyringCredentialValue(key string) (string, bool) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", false
	}

	svc, err := ss.NewSecretService()
	if err != nil {
		return "", false
	}
	defer svc.Conn.Close()

	collection := svc.GetLoginCollection()
	search := map[string]string{
		"username": key,
		"service":  credentialsKeyringService,
	}
	if err := svc.Unlock(collection.Path()); err != nil {
		return "", false
	}
	results, err := svc.SearchItems(collection, search)
	if err != nil || len(results) == 0 {
		return "", false
	}

	session, err := svc.OpenSession()
	if err != nil {
		return "", false
	}
	defer svc.Close(session)

	if err := svc.Unlock(results[0]); err != nil {
		return "", false
	}
	secret, err := svc.GetSecret(results[0], session.Path())
	if err != nil || secret == nil || len(secret.Value) == 0 {
		return "", false
	}
	return string(secret.Value), true
}
