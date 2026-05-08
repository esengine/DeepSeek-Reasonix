---
name: Display / rendering issue
about: Screen flicker, garbled output, leftover artifacts, cursor jumping
labels: bug, rendering
---

> Display problems almost always come from the **terminal emulator**, not
> the shell. Please fill the terminal section carefully — `bash vs PowerShell`
> tells us very little; `VSCode integrated terminal vs Windows Terminal`
> tells us everything.

**Symptom** (tick all that apply)
- [ ] Whole screen flickers / flashes during streaming response
- [ ] Lines tear or only half-redraw
- [ ] Stale output left behind after a frame updates
- [ ] Cursor jumps to wrong column or vanishes
- [ ] Mojibake / wrong-width characters (e.g. `□`, half-width emoji)
- [ ] Other (describe below)

**When it happens**
- [ ] During assistant streaming (token-by-token output)
- [ ] When tool cards expand / collapse
- [ ] During scroll-up / scrollback
- [ ] On terminal resize
- [ ] On launch / on quit
- [ ] Other (describe below)

**Terminal — the important part**

Where exactly are you running `reasonix`?

- [ ] **VSCode** integrated terminal — VSCode version: `?`
- [ ] **Cursor** integrated terminal — Cursor version: `?`
- [ ] **Windows Terminal** — version: `?`
- [ ] **cmd.exe** (legacy console host)
- [ ] **PowerShell ISE** (note: ISE doesn't support ANSI — most things will look broken)
- [ ] **iTerm2** / **Terminal.app** / **Alacritty** / **kitty** / **WezTerm** / **Hyper**
- [ ] tmux / screen / mosh — and inside which outer terminal? `?`
- [ ] Other:

> 💡 **How to find your VSCode / Cursor version**
> `Help → About` (Windows/Linux) or `Code → About Visual Studio Code` (macOS).
> Paste the whole panel — version + commit + Electron + xterm.js if shown.

**Diagnostic dump — copy/paste output**

Run **one** of the snippets below in the same terminal where you saw the
issue, and paste the output here:

<details><summary>PowerShell (Windows)</summary>

```powershell
reasonix --version; node --version
$PSVersionTable.PSVersion.ToString()
[System.Environment]::OSVersion.VersionString
"TERM=$env:TERM"
"TERM_PROGRAM=$env:TERM_PROGRAM"
"TERM_PROGRAM_VERSION=$env:TERM_PROGRAM_VERSION"
"COLORTERM=$env:COLORTERM"
"WT_SESSION=$env:WT_SESSION"
"VSCODE_INJECTION=$env:VSCODE_INJECTION"
"WSL_DISTRO_NAME=$env:WSL_DISTRO_NAME"
```

</details>

<details><summary>bash / zsh (macOS / Linux / WSL / Git Bash)</summary>

```bash
reasonix --version; node --version
uname -a
echo "TERM=$TERM"
echo "TERM_PROGRAM=$TERM_PROGRAM"
echo "TERM_PROGRAM_VERSION=$TERM_PROGRAM_VERSION"
echo "COLORTERM=$COLORTERM"
echo "WT_SESSION=$WT_SESSION"
echo "VSCODE_INJECTION=$VSCODE_INJECTION"
echo "WSL_DISTRO_NAME=$WSL_DISTRO_NAME"
```

</details>

```
<paste output here>
```

**VSCode / Cursor users only — terminal settings**

Open Settings (`Ctrl+,`), search `terminal.integrated.gpuAcceleration`,
report current value: `auto` / `on` / `canvas` / `off` — `?`

Already tried any of:
- [ ] Switching `gpuAcceleration` to a different value
- [ ] Detaching the terminal (drag tab into its own window)
- [ ] Running the same command in a non-VSCode terminal — did it still flicker? `yes / no`

**Reproduction**

Steps that reliably trigger it (commands run, files edited, was a tool
streaming a long response, was the window being resized, …):

1.
2.
3.

**Screen recording (strongly preferred)**

A 5–10s GIF or MP4 is worth 1000 words for rendering bugs. Drop it in
this comment box — GitHub uploads attachments inline.
