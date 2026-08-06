//go:build (dragonfly && cgo) || (freebsd && cgo) || linux || netbsd || openbsd

package config

import (
	"context"
	"fmt"
	"strings"

	dbus "github.com/godbus/dbus/v5"
)

// Secret Service constants (same service Reasonix historically used via
// zalando/go-keyring). Every D-Bus call uses the shared migration context so a
// stuck bus cannot hang CLI startup past the batch deadline.
const (
	ssServiceName         = "org.freedesktop.secrets"
	ssServicePath         = "/org/freedesktop/secrets"
	ssServiceInterface    = "org.freedesktop.Secret.Service"
	ssCollectionInterface = "org.freedesktop.Secret.Collection"
	ssItemInterface       = "org.freedesktop.Secret.Item"
	ssSessionInterface    = "org.freedesktop.Secret.Session"
	ssCollectionsIface    = "org.freedesktop.Secret.Service"
	ssCollectionsProp     = "Collections"
	ssLoginCollection     = "/org/freedesktop/secrets/collection/login"
	ssLoginAlias          = "/org/freedesktop/secrets/aliases/default"
	ssPropertiesInterface = "org.freedesktop.DBus.Properties"
)

type ssSecret struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string `dbus:"content_type"`
}

// legacyKeyringProbe reads one legacy credential from Secret Service under ctx.
// It opens a private, caller-owned session-bus connection (never dbus.SessionBus),
// bounds Dial/Auth/Hello by ctx, routes every method/property call through
// CallWithContext(ctx), and always closes the connection before returning so
// godbus worker goroutines cannot leak into goleak-checked tests.
func legacyKeyringProbe(ctx context.Context, key string) legacyKeyringOutcome {
	key = strings.TrimSpace(key)
	if key == "" {
		return legacyKeyringOutcome{Status: legacyKeyringAbsent}
	}
	if err := ctx.Err(); err != nil {
		return legacyKeyringOutcome{Status: legacyKeyringTimeout}
	}

	conn, err := openPrivateSessionBus(ctx)
	if err != nil {
		return mapKeyringCtxErr(ctx, err)
	}
	defer func() { _ = conn.Close() }()

	svc := conn.Object(ssServiceName, ssServicePath)
	collectionPath, err := ssResolveLoginCollection(ctx, svc)
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
	// Always close the Secret Service session with the remaining budget (never
	// context.Background) so a stuck Close still respects the migration deadline.
	defer func() {
		session := conn.Object(ssServiceName, sessionPath)
		_ = session.CallWithContext(ctx, ssSessionInterface+".Close", 0).Err
	}()

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

// openPrivateSessionBus dials a private session-bus connection (Auth+Hello)
// without using the process-global SessionBus cache. Dial/Auth/Hello are not
// context-aware in godbus, so the whole connect is raced against ctx and any
// late connection is closed to reclaim godbus worker goroutines.
func openPrivateSessionBus(ctx context.Context) (*dbus.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	type result struct {
		conn *dbus.Conn
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		// Private connection: ConnectSessionBus = Dial + Auth + Hello, not shared.
		conn, err := dbus.ConnectSessionBus()
		ch <- result{conn: conn, err: err}
	}()
	select {
	case <-ctx.Done():
		// Reap a late success so workers cannot leak past the deadline.
		go func() {
			r := <-ch
			if r.conn != nil {
				_ = r.conn.Close()
			}
		}()
		return nil, ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return nil, r.err
		}
		return r.conn, nil
	}
}

func ssResolveLoginCollection(ctx context.Context, svc dbus.BusObject) (dbus.ObjectPath, error) {
	path := dbus.ObjectPath(ssLoginCollection)
	val, err := ssGetProperty(ctx, svc, ssCollectionsIface, ssCollectionsProp)
	if err != nil {
		// Fall back to the default alias when Collections is unavailable.
		return dbus.ObjectPath(ssLoginAlias), nil
	}
	paths, _ := val.Value().([]dbus.ObjectPath)
	for _, p := range paths {
		if p == path {
			return path, nil
		}
	}
	return dbus.ObjectPath(ssLoginAlias), nil
}

// ssGetProperty is CallWithContext-based Properties.Get. BusObject.GetProperty
// uses a non-context Call and would escape the migration deadline.
func ssGetProperty(ctx context.Context, obj dbus.BusObject, iface, name string) (dbus.Variant, error) {
	var val dbus.Variant
	err := obj.CallWithContext(ctx, ssPropertiesInterface+".Get", 0, iface, name).Store(&val)
	if err != nil {
		return dbus.Variant{}, err
	}
	return val, nil
}

func ssUnlock(ctx context.Context, svc dbus.BusObject, target dbus.ObjectPath) error {
	var unlocked []dbus.ObjectPath
	var prompt dbus.ObjectPath
	if err := svc.CallWithContext(ctx, ssServiceInterface+".Unlock", 0, []dbus.ObjectPath{target}).Store(&unlocked, &prompt); err != nil {
		return err
	}
	// Migration must not wait on an interactive prompt (would hang CLI startup).
	if prompt != "/" && prompt != "" {
		for _, p := range unlocked {
			if p == target || target == dbus.ObjectPath(ssLoginAlias) {
				return nil
			}
		}
		return fmt.Errorf("secret service unlock requires interactive prompt")
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
