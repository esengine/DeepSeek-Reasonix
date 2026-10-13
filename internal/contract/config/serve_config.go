package config

// ServeConfig controls the HTTP serve frontend security settings.
type ServeConfig struct {
	// AuthMode is "none" (default), "token" (a pre-shared token in the URL
	// query string) or "password" (a login page with bcrypt verification).
	AuthMode string `toml:"auth_mode"`
	// Token is the pre-shared token for auth_mode = "token"; empty generates a
	// random one at startup and prints it.
	Token string `toml:"token"`
	// PasswordHash is the bcrypt hash for auth_mode = "password"; make one with
	// `reasonix serve --hash-password --password '...'`.
	PasswordHash string `toml:"password_hash"`
	// BehindProxy marks a trusted reverse proxy that sets X-Forwarded-For and
	// X-Forwarded-Proto. Off (default) those headers are ignored, since an
	// attacker can otherwise forge them.
	BehindProxy bool `toml:"behind_proxy"`
	// SharePort fixes the port of the phone-access LAN link; zero picks a free
	// one each time the door opens. User-global like the rest of [serve].
	SharePort int `toml:"share_port"`
	// RememberPairedDevices keeps paired phones across restarts: the next process
	// reopens the share where it was and adopts the devices that paired against it.
	// Off by default, like every other promise about a device you are not holding.
	RememberPairedDevices bool `toml:"remember_paired_devices"`
}
