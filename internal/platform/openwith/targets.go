package openwith

// recipe is one declared way of starting a program on one operating system.
type recipe struct {
	// exes are executable paths, with %NAME% environment roots and * globs.
	exes []string
	// apps are macOS application bundle names, started through `open -a`.
	apps []string
	// commands are names looked up on PATH.
	commands []string
	// lead is the argument list before the folder; glue is spelled directly
	// before it in the same argument (`--working-directory=`).
	lead []string
	glue string
}

type target struct {
	id   string
	name string
	by   map[string][]recipe
}

var targets = []target{
	{id: "file-manager", name: "File manager", by: map[string][]recipe{
		"windows": {{exes: []string{`%SystemRoot%\explorer.exe`}}},
		"darwin":  {{exes: []string{"/usr/bin/open"}}},
		"linux":   {{commands: []string{"xdg-open"}}},
	}},
	{id: "vscode", name: "Visual Studio Code", by: map[string][]recipe{
		"windows": {{exes: []string{`%LOCALAPPDATA%\Programs\Microsoft VS Code\Code.exe`, `%ProgramFiles%\Microsoft VS Code\Code.exe`}}},
		"darwin":  {{apps: []string{"Visual Studio Code"}}},
		"linux":   {{commands: []string{"code"}}},
	}},
	{id: "cursor", name: "Cursor", by: map[string][]recipe{
		"windows": {{exes: []string{`%LOCALAPPDATA%\Programs\cursor\Cursor.exe`, `%ProgramFiles%\Cursor\Cursor.exe`}}},
		"darwin":  {{apps: []string{"Cursor"}}},
		"linux":   {{commands: []string{"cursor"}}},
	}},
	{id: "idea", name: "IntelliJ IDEA", by: map[string][]recipe{
		"windows": {{exes: []string{
			`%ProgramFiles%\JetBrains\IntelliJ IDEA*\bin\idea64.exe`,
			`%LOCALAPPDATA%\Programs\IntelliJ IDEA*\bin\idea64.exe`,
			`%LOCALAPPDATA%\JetBrains\Toolbox\apps\IDEA-*\ch-*\*\bin\idea64.exe`,
		}}},
		"darwin": {{apps: []string{"IntelliJ IDEA", "IntelliJ IDEA CE"}}},
		"linux":  {{commands: []string{"idea", "intellij-idea-ultimate", "intellij-idea-community"}}},
	}},
	{id: "terminal", name: "Terminal", by: map[string][]recipe{
		"windows": {{exes: []string{`%LOCALAPPDATA%\Microsoft\WindowsApps\wt.exe`}, lead: []string{"-d"}}},
		"darwin":  {{apps: []string{"Terminal"}}},
		"linux": {
			{commands: []string{"xdg-terminal-exec"}, glue: "--dir="},
			{commands: []string{"gnome-terminal"}, glue: "--working-directory="},
			{commands: []string{"konsole"}, lead: []string{"--workdir"}},
			{commands: []string{"xfce4-terminal"}, glue: "--working-directory="},
			{commands: []string{"kitty"}, lead: []string{"--directory"}},
			{commands: []string{"alacritty"}, lead: []string{"--working-directory"}},
		},
	}},
}
