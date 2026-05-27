// Terminal identification is needed to gate features (OSC 8 hyperlinks,
// Kitty keyboard protocol, sixel graphics, etc.). Detection runs once at
// module load: the host terminal does not change mid-process, and a static
// snapshot keeps every call site cheap.

type TerminalKind =
  | 'kitty'
  | 'iterm'
  | 'wezterm'
  | 'vscode'
  | 'tmux'
  | 'ghostty'
  | 'windows-terminal'
  | '';

function detectTerminal(): TerminalKind {
  const termProgram = process.env['TERM_PROGRAM'] ?? '';
  if (termProgram === 'iTerm.app') return 'iterm';
  if (termProgram === 'vscode') return 'vscode';
  if (termProgram === 'WezTerm') return 'wezterm';
  if (termProgram === 'ghostty') return 'ghostty';

  // Kitty doesn't set TERM_PROGRAM, but always sets either KITTY_WINDOW_ID
  // or a TERM containing "kitty" (e.g. xterm-kitty).
  if (process.env['KITTY_WINDOW_ID'] || process.env['TERM']?.includes('kitty')) {
    return 'kitty';
  }

  // Windows Terminal exposes WT_SESSION. Check before tmux because tmux can
  // legitimately run inside Windows Terminal and we want the outer host.
  if (process.env['WT_SESSION']) return 'windows-terminal';
  if (process.env['TMUX']) return 'tmux';
  return '';
}

export const env = {
  terminal: detectTerminal(),
} as const;
