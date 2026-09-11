module reasonix/desktop

go 1.26.0

toolchain go1.26.6

// The desktop service is a nested module so its CGO build never touches the
// CLI's CGO_ENABLED=0 single-static-binary guarantee. The replace lets it import
// the same reasonix/internal/* kernel (the import path stays under reasonix/, so
// the internal rule still permits it). `go mod tidy` here resolves its
// transitive deps; the parent module's go build/test ./... skips this directory.
require reasonix v0.0.0

require (
	aead.dev/minisign v0.3.0
	fyne.io/systray v1.12.3-0.20260814134402-f60f01be81c6
	github.com/BurntSushi/toml v1.6.0
	github.com/UserExistsError/conpty v0.1.4
	github.com/creack/pty v1.1.24
	github.com/fsnotify/fsnotify v1.10.1
	github.com/go-ole/go-ole v1.3.0
	github.com/godbus/dbus/v5 v5.2.2
	github.com/google/uuid v1.6.0
	github.com/tc-hib/winres v0.3.1
	golang.org/x/crypto v0.56.0
	golang.org/x/image v0.45.0
	golang.org/x/mod v0.41.0
	golang.org/x/sync v0.23.0
	golang.org/x/sys v0.48.0
	golang.org/x/text v0.41.0
)

require (
	git.sr.ht/~jackmordaunt/go-toast/v2 v2.0.3 // indirect
	github.com/aymanbagabas/go-udiff v0.4.1 // indirect
	github.com/bmatcuk/doublestar/v4 v4.10.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/danieljoos/wincred v1.2.3 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/joho/godotenv v1.5.1 // indirect
	github.com/kevinburke/ssh_config v1.6.0 // indirect
	github.com/kr/fs v0.1.0 // indirect
	github.com/larksuite/oapi-sdk-go/v3 v3.11.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/mattn/go-pointer v0.0.1 // indirect
	github.com/mattn/go-runewidth v0.0.29 // indirect
	github.com/modelcontextprotocol/go-sdk v1.8.0-pre.2 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/nfnt/resize v0.0.0-20180221191011-83c6a9932646 // indirect
	github.com/pkg/sftp v1.13.11 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/sabhiram/go-gitignore v0.0.0-20210923224102-525f6e181f06 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3 // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/sky-valley/pi v0.84.20 // indirect
	github.com/tree-sitter/go-tree-sitter v0.25.0 // indirect
	github.com/tree-sitter/tree-sitter-javascript v0.25.0 // indirect
	github.com/tree-sitter/tree-sitter-python v0.25.0 // indirect
	github.com/tree-sitter/tree-sitter-rust v0.24.2 // indirect
	github.com/tree-sitter/tree-sitter-typescript v0.23.2 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	github.com/yuin/goldmark v1.8.6 // indirect
	github.com/zalando/go-keyring v0.2.8 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/oauth2 v0.37.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	modernc.org/libc v1.75.6 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
	modernc.org/sqlite v1.58.0 // indirect
	mvdan.cc/sh/v3 v3.14.0 // indirect
)

replace reasonix => ../

// Reasonix keeps one signed Windows tray identity across versioned desktop paths.
replace fyne.io/systray => ./third_party/systray
