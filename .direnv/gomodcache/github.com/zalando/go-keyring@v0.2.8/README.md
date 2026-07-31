# Go Keyring library

[![Go Report Card](https://goreportcard.com/badge/github.com/zalando/go-keyring)](https://goreportcard.com/report/github.com/zalando/go-keyring)
[![GoDoc](https://godoc.org/github.com/zalando/go-keyring?status.svg)](https://godoc.org/github.com/zalando/go-keyring)

`go-keyring` is an OS-agnostic library for *setting*, *getting* and *deleting*
secrets from the system keyring. It supports **OS X**, **Linux/BSD (dbus)** and
**Windows**.

go-keyring was created after its authors searched for, but couldn't find, a better alternative. It aims to simplify
using statically linked binaries, which is cumbersome when relying on C bindings (as other keyring libraries do).

#### Potential Uses

If you're working with an application that needs to store user credentials
locally on the user's machine, go-keyring might come in handy. For instance, if you are writing a CLI for an API
that requires a username and password, you can store this information in the
keyring instead of having the user type it on every invocation.

## Dependencies

#### OS X

The OS X implementation depends on the `/usr/bin/security` binary for
interfacing with the OS X keychain. It should be available by default.

#### Linux and *BSD

The Linux and *BSD implementation depends on the [Secret Service][SecretService] dbus
interface, which is provided by [GNOME Keyring](https://wiki.gnome.org/Projects/GnomeKeyring).

It's expected that the default collection `login` exists in the keyring, because
it's the default in most distros. If it doesn't exist, you can create it through the
keyring frontend program [Seahorse](https://wiki.gnome.org/Apps/Seahorse):

* Open `seahorse`
* Go to **File > New > Password Keyring**
* Click **Continue**
* When asked for a name, use: **login**

## Example Usage

How to *set* and *get* a secret from the keyring:

```go
package main

import (
    "log"

    "github.com/zalando/go-keyring"
)

func main() {
    service := "my-app"
    user := "anon"
    password := "secret"

    // set password
    err := keyring.Set(service, user, password)
    if err != nil {
        log.Fatal(err)
    }

    // get password
    secret, err := keyring.Get(service, user)
    if err != nil {
        log.Fatal(err)
    }

    log.Println(secret)
}

```

## Direct CLI Usage

While this library provides a convenient Go API, you can also interact with the system keyring directly using OS-specific command-line tools. This can be useful for debugging, scripting, or understanding what the library does under the hood. You can use the CLI to set-up the secrets from a script and then access them from Go, or vice-versa.

### macOS

macOS uses the `security` command to interact with the Keychain.

**Set a password:**
```bash
security add-generic-password -U -s "service" -a "user" -w "password"
```

**Get a password:**
```bash
security find-generic-password -s "service" -wa "user"
```

**Delete a password:**
```bash
security delete-generic-password -s "service" -a "user"
```

Where:
- `-s` specifies the service name
- `-a` specifies the account/username
- `-w` specifies the password to store
- `-U` updates the password if it already exists
- The `w` option in `-wa` outputs only the password value

### Linux and *BSD

Linux and *BSD systems use the Secret Service API via D-Bus. The easiest way to interact with it from the command line is using `secret-tool`, which is part of libsecret.

**Install secret-tool (if not already installed):**
```bash
# Debian/Ubuntu
sudo apt-get install libsecret-tools

# Fedora/RHEL
sudo dnf install libsecret

# Arch Linux
sudo pacman -S libsecret
```

**Set a password:**
```bash
secret-tool store --label="Password for 'user' on 'service'" service "service" username "user"
# You'll be prompted to enter the password
```

Or provide the password directly:
```bash
echo -n "password" | secret-tool store --label="Password for 'user' on 'service'" service "service" username "user"
```

**Get a password:**
```bash
secret-tool lookup service "service" username "user"
```

**Delete a password:**
```bash
secret-tool clear service "service" username "user"
```

Note: The `service` and `username` are attributes used to identify the secret. The label is a human-readable description.

### Windows

Windows uses the Credential Manager, which can be accessed via `cmdkey` or PowerShell.

**Using cmdkey:**

**Set a password:**
```cmd
cmdkey /generic:"service:user" /user:"user" /pass:"password"
```

**Get a password:**

`cmdkey` doesn't support retrieving passwords directly. Use PowerShell instead:
```powershell
$cred = Get-StoredCredential -Target "service:user"
$cred.GetNetworkCredential().Password
```

Or using the Windows API via PowerShell:
```powershell
[System.Net.NetworkCredential]::new("", (Get-StoredCredential -Target "service:user").Password).Password
```

**Delete a password:**
```cmd
cmdkey /delete:"service:user"
```

**Using PowerShell with CredentialManager module:**

First, install the CredentialManager module:
```powershell
Install-Module -Name CredentialManager -Force
```

**Set a password:**
```powershell
New-StoredCredential -Target "service:user" -UserName "user" -Password "password" -Type Generic -Persist LocalMachine
```

**Get a password:**
```powershell
(Get-StoredCredential -Target "service:user").GetNetworkCredential().Password
```

**Delete a password:**
```powershell
Remove-StoredCredential -Target "service:user"
```

Note: On Windows, the library combines the service and username as `service:username` for the credential target name.

## Tests

### Running tests

Running the tests is simple:

```
go test
```

Which OS you use *does* matter. If you're using **Linux** or **BSD**, it will
test the implementation in `keyring_unix.go`. If running the tests
on **OS X**, it will test the implementation in `keyring_darwin.go`.

### Mocking

If you need to mock the keyring behavior for testing on systems without a keyring implementation you can call `MockInit()` which will replace the OS defined provider with an in-memory one.

```go
package implementation

import (
    "testing"

    "github.com/zalando/go-keyring"
)

func TestMockedSetGet(t *testing.T) {
    keyring.MockInit()
    err := keyring.Set("service", "user", "password")
    if err != nil {
        t.Fatal(err)
    }

    p, err := keyring.Get("service", "user")
    if err != nil {
        t.Fatal(err)
    }

    if p != "password" {
        t.Error("password was not the expected string")
    }

}

```

## Contributing/TODO

We welcome contributions from the community; please use [CONTRIBUTING.md](CONTRIBUTING.md) as your guidelines for getting started. Here are some items that we'd love help with:

* The code base
* Better test coverage

Please use GitHub issues as the starting point for contributions, new ideas and/or bug reports.

## Contact

* E-Mail: <team-teapot@zalando.de>
* Security issues: Please send an email to the [maintainers](MAINTAINERS), and we'll try to get back to you within two workdays. If you don't hear back, send an email to <team-teapot@zalando.de> and someone will respond within five days max.

## Contributors

Thanks to:

* [your name here]

## License

See [LICENSE](LICENSE) file.

[SecretService]: https://specifications.freedesktop.org/secret-service-spec/latest/
