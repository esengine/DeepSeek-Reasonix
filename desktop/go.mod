module reasonix/desktop

go 1.26.0

toolchain go1.26.10

// The desktop tools are a nested module so their CGO build never touches the
// CLI's CGO_ENABLED=0 single-static-binary guarantee. The replace lets them
// import the same reasonix/internal/* kernel (the import path stays under
// reasonix/, so the internal rule still permits it); the parent module's
// go build/test ./... skips this directory.
require reasonix v0.0.0

require (
	aead.dev/minisign v0.3.0
	golang.org/x/mod v0.42.0
	golang.org/x/sys v0.49.0
)

require (
	github.com/BurntSushi/toml v1.6.0 // indirect
	github.com/aymanbagabas/go-udiff v0.4.1 // indirect
	github.com/bmatcuk/doublestar/v4 v4.10.3 // indirect
	github.com/danieljoos/wincred v1.2.3 // indirect
	github.com/dustin/go-humanize v1.1.0 // indirect
	github.com/godbus/dbus/v5 v5.2.4 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/joho/godotenv v1.5.1 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.1.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3 // indirect
	github.com/zalando/go-keyring v0.2.8 // indirect
	golang.org/x/crypto v0.58.0 // indirect
	golang.org/x/net v0.61.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	modernc.org/libc v1.79.0 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
	modernc.org/sqlite v1.61.0 // indirect
	mvdan.cc/sh/v3 v3.14.1 // indirect
)

replace reasonix => ../
