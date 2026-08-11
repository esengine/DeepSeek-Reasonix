//go:build (dragonfly && cgo) || (freebsd && cgo) || linux || netbsd || openbsd

package config

import (
	"context"
	"os"
	"strings"
	"time"

	dbus "github.com/godbus/dbus/v5"
	ss "github.com/zalando/go-keyring/secret_service"
)

const legacyKeyringProbeTimeout = 2 * time.Second

func legacyKeyringCredentialValue(key string) (string, bool) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", false
	}
	// A session bus is not available on most headless hosts. Avoid creating a
	// Secret Service connection there because the provider can wait forever for
	// a reply from a bus that has no secrets service.
	if strings.TrimSpace(os.Getenv("DBUS_SESSION_BUS_ADDRESS")) == "" {
		return "", false
	}

	svc, err := ss.NewSecretService()
	if err != nil {
		return "", false
	}
	defer svc.Conn.Close()

	collection, err := legacyKeyringLoginCollection(svc)
	if err != nil {
		return "", false
	}
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

func legacyKeyringLoginCollection(svc *ss.SecretService) (dbus.BusObject, error) {
	ctx, cancel := context.WithTimeout(context.Background(), legacyKeyringProbeTimeout)
	defer cancel()

	var value dbus.Variant
	err := svc.Conn.Object("org.freedesktop.secrets", "/org/freedesktop/secrets").
		CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0,
			"org.freedesktop.Secret.Service.Collections").Store(&value)
	if err != nil {
		return nil, err
	}
	paths, ok := value.Value().([]dbus.ObjectPath)
	if !ok {
		return nil, dbus.ErrMsgInvalidArg
	}

	login := dbus.ObjectPath("/org/freedesktop/secrets/collection/login")
	for _, path := range paths {
		if path == login {
			return svc.Object("org.freedesktop.secrets", path), nil
		}
	}
	return svc.Object("org.freedesktop.secrets", "/org/freedesktop/secrets/aliases/default"), nil
}
