//go:build (dragonfly && cgo) || (freebsd && cgo) || linux || netbsd || openbsd

package config

import (
	"context"
	"strings"

	dbus "github.com/godbus/dbus/v5"
)

// Secret Service constants (same service Reasonix historically used via
// zalando/go-keyring). We call CallWithContext so a shared migration budget can
// cancel stuck D-Bus replies without an external helper process.
const (
	ssServiceName         = "org.freedesktop.secrets"
	ssServicePath         = "/org/freedesktop/secrets"
	ssServiceInterface    = "org.freedesktop.Secret.Service"
	ssCollectionInterface = "org.freedesktop.Secret.Collection"
	ssItemInterface       = "org.freedesktop.Secret.Item"
	ssSessionInterface    = "org.freedesktop.Secret.Session"
	ssCollectionsProp     = "org.freedesktop.Secret.Service.Collections"
	ssLoginCollection     = "/org/freedesktop/secrets/collection/login"
	ssLoginAlias          = "/org/freedesktop/secrets/aliases/default"
)

type ssSecret struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string `dbus:"content_type"`
}

// legacyKeyringProbe reads one legacy credential from Secret Service under ctx.
// ctx cancellation maps to timeout; transport/unlock failures map to error;
// empty search / empty secret map to absent.
func legacyKeyringProbe(ctx context.Context, key string) legacyKeyringOutcome {
	key = strings.TrimSpace(key)
	if key == "" {
		return legacyKeyringOutcome{Status: legacyKeyringAbsent}
	}
	if err := ctx.Err(); err != nil {
		return legacyKeyringOutcome{Status: legacyKeyringTimeout}
	}

	conn, err := dbus.SessionBus()
	if err != nil {
		return mapKeyringCtxErr(ctx, err)
	}
	// Do not close SessionBus: it is a shared process connection.

	svc := conn.Object(ssServiceName, ssServicePath)
	collectionPath, err := ssResolveLoginCollection(ctx, conn, svc)
	if err != nil {
		return mapKeyringCtxErr(ctx, err)
	}
	if err := ssUnlock(ctx, svc, collectionPath); err != nil {
		return mapKeyringCtxErr(ctx, err)
	}

	collection := conn.Object(ssServiceName, collectionPath)
	search := map[string]string{
		"username": key,
		"service":  credentialsKeyringService,
	}
	var results []dbus.ObjectPath
	if err := collection.CallWithContext(ctx, ssCollectionInterface+".SearchItems", 0, search).Store(&results); err != nil {
		return mapKeyringCtxErr(ctx, err)
	}
	if len(results) == 0 {
		return legacyKeyringOutcome{Status: legacyKeyringAbsent}
	}

	var disregard dbus.Variant
	var sessionPath dbus.ObjectPath
	if err := svc.CallWithContext(ctx, ssServiceInterface+".OpenSession", 0, "plain", dbus.MakeVariant("")).Store(&disregard, &sessionPath); err != nil {
		return mapKeyringCtxErr(ctx, err)
	}
	session := conn.Object(ssServiceName, sessionPath)
	defer func() { _ = session.CallWithContext(context.Background(), ssSessionInterface+".Close", 0).Err }()

	if err := ssUnlock(ctx, svc, results[0]); err != nil {
		return mapKeyringCtxErr(ctx, err)
	}

	var secret ssSecret
	item := conn.Object(ssServiceName, results[0])
	if err := item.CallWithContext(ctx, ssItemInterface+".GetSecret", 0, sessionPath).Store(&secret); err != nil {
		return mapKeyringCtxErr(ctx, err)
	}
	if len(secret.Value) == 0 {
		return legacyKeyringOutcome{Status: legacyKeyringAbsent}
	}
	return legacyKeyringOutcome{Status: legacyKeyringFound, Value: string(secret.Value)}
}

func ssResolveLoginCollection(ctx context.Context, conn *dbus.Conn, svc dbus.BusObject) (dbus.ObjectPath, error) {
	path := dbus.ObjectPath(ssLoginCollection)
	val, err := svc.GetProperty(ssCollectionsProp)
	if err != nil {
		// Fall back to the default alias when Collections is unavailable.
		return dbus.ObjectPath(ssLoginAlias), nil
	}
	_ = ctx
	paths, _ := val.Value().([]dbus.ObjectPath)
	for _, p := range paths {
		if p == path {
			return path, nil
		}
	}
	return dbus.ObjectPath(ssLoginAlias), nil
}

func ssUnlock(ctx context.Context, svc dbus.BusObject, target dbus.ObjectPath) error {
	var unlocked []dbus.ObjectPath
	var prompt dbus.ObjectPath
	if err := svc.CallWithContext(ctx, ssServiceInterface+".Unlock", 0, []dbus.ObjectPath{target}).Store(&unlocked, &prompt); err != nil {
		return err
	}
	// Migration must not wait on an interactive prompt (would hang CLI startup).
	if prompt != "/" && prompt != "" {
		// If Unlock already returned the object, accept it; otherwise fail closed.
		for _, p := range unlocked {
			if p == target || target == dbus.ObjectPath(ssLoginAlias) {
				return nil
			}
		}
		return context.Canceled // surfaces as error unless ctx already timed out
	}
	return nil
}

func mapKeyringCtxErr(ctx context.Context, err error) legacyKeyringOutcome {
	if err == nil {
		return legacyKeyringOutcome{Status: legacyKeyringAbsent}
	}
	if ctx.Err() != nil {
		return legacyKeyringOutcome{Status: legacyKeyringTimeout}
	}
	return legacyKeyringOutcome{Status: legacyKeyringError}
}
