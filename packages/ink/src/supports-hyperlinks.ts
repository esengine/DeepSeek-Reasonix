import supportsHyperlinksLib from 'supports-hyperlinks';

export const ADDITIONAL_HYPERLINK_TERMINALS: ReadonlySet<string> = new Set([
  'ghostty',
  'Hyper',
  'kitty',
  'alacritty',
  'iTerm.app',
  'iTerm2',
]);

type EnvLike = Record<string, string | undefined>;

type SupportsHyperlinksOptions = {
  env?: EnvLike;
  stdoutSupported?: boolean;
};

/** Whether the current stdout can emit clickable OSC 8 hyperlinks. */
export function supportsHyperlinks(options?: SupportsHyperlinksOptions): boolean {
  const stdoutSupported = options?.stdoutSupported ?? supportsHyperlinksLib.stdout;
  if (stdoutSupported) return true;

  const env = options?.env ?? process.env;

  const termProgram = env['TERM_PROGRAM'];
  if (termProgram && ADDITIONAL_HYPERLINK_TERMINALS.has(termProgram)) {
    return true;
  }

  const lcTerminal = env['LC_TERMINAL'];
  if (lcTerminal && ADDITIONAL_HYPERLINK_TERMINALS.has(lcTerminal)) {
    return true;
  }

  // Kitty advertises itself solely through TERM=xterm-kitty.
  const term = env['TERM'];
  if (term?.includes('kitty')) return true;

  return false;
}
