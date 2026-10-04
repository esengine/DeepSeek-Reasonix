package bootstrap

import "testing"

func TestResolveWorkspace(t *testing.T) {
	cases := []struct {
		name      string
		target    remoteOS
		home      string
		workspace string
		want      string
	}{
		{"posix empty", posixShell{}, "/home/u", "", "/home/u"},
		{"posix tilde", posixShell{}, "/home/u", "~", "/home/u"},
		{"posix tilde sub", posixShell{}, "/home/u", "~/a b", "/home/u/a b"},
		{"posix relative", posixShell{}, "/home/u", "proj", "/home/u/proj"},
		{"posix absolute", posixShell{}, "/home/u", "/srv/it's", "/srv/it's"},
		{"win empty", windowsShell{}, "/C:/Users/u", "", "/C:/Users/u"},
		{"win tilde sub", windowsShell{}, "/C:/Users/u", "~/Desktop/x", "/C:/Users/u/Desktop/x"},
		{"win relative", windowsShell{}, "/C:/Users/u", "Desktop/x", "/C:/Users/u/Desktop/x"},
		{"win drive backslash", windowsShell{}, "/C:/Users/u", `C:\Users\u\Desktop\x`, "/C:/Users/u/Desktop/x"},
		{"win drive slash", windowsShell{}, "/C:/Users/u", "C:/Users/u", "/C:/Users/u"},
		{"win drive other", windowsShell{}, "/C:/Users/u", `D:\work y`, "/D:/work y"},
		{"win sftp spelling", windowsShell{}, "/C:/Users/u", "/C:/Users/u/x", "/C:/Users/u/x"},
		{"win lower drive", windowsShell{}, "/C:/Users/u", "d:/x", "/D:/x"},
		{"win trailing sep", windowsShell{}, "/C:/Users/u", `C:\Users\u\`, "/C:/Users/u"},
		{"win extended prefix", windowsShell{}, "/C:/Users/u", `\\?\C:\x`, "/C:/x"},
		{"win drive relative", windowsShell{}, "/C:/Users/u", "C:foo", "/C:/Users/u/C:foo"},
		{"win bare drive", windowsShell{}, "/C:/Users/u", "C:", "/C:"},
		{"win rooted", windowsShell{}, "/C:/Users/u", `\foo`, "/foo"},
		{"win tilde backslash", windowsShell{}, "/C:/Users/u", `~\x`, "/C:/Users/u/x"},
		{"posix root home", posixShell{}, "/", "", "/"},
		{"posix root home tilde", posixShell{}, "/", "~", "/"},
		{"posix backslash tilde is a name", posixShell{}, "/home/u", `~\x`, `/home/u/~\x`},
		{"win unc", windowsShell{}, "/C:/Users/u", `\\srv\share\x`, "//srv/share/x"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := resolveWorkspace(c.target, c.workspace, c.home); got != c.want {
				t.Fatalf("resolveWorkspace(%q) = %q, want %q", c.workspace, got, c.want)
			}
		})
	}
}

func TestWindowsWorkspaceRoundTripsToShell(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"drive backslash", `C:\Users\u\Desktop\x`, `C:\Users\u\Desktop\x`},
		{"drive slash", "C:/Users/u/Desktop/x", `C:\Users\u\Desktop\x`},
		{"sftp drive", "/C:/Users/u/Desktop/x", `C:\Users\u\Desktop\x`},
		{"UNC backslash", `\\srv\share\x`, `\\srv\share\x`},
		{"UNC slash", "//srv/share/x", `\\srv\share\x`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := (windowsShell{}).NativePath(c.in); got != c.want {
				t.Fatalf("NativePath(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
