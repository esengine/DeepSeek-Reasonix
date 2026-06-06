/** Operating System Command (OSC) generators and parsers. */

import { Buffer } from 'buffer'
import { env } from '../_internal/env.js'
import { execFileNoThrow } from '../_internal/execFileNoThrow.js'
import { BEL, ESC, ESC_TYPE, SEP } from './ansi.js'
import type { Action, Color, TabStatusAction } from './types.js'

export const OSC_PREFIX = ESC + String.fromCharCode(ESC_TYPE.OSC)

/** String Terminator (ESC \\). The spec-correct OSC terminator. */
export const ST = ESC + '\\'

/** Build an OSC sequence: `ESC ] p1;p2;…;pN <terminator>`. */
export function osc(...parts: (string | number)[]): string {
  const terminator = env.terminal === 'kitty' ? ST : BEL
  return `${OSC_PREFIX}${parts.join(SEP)}${terminator}`
}

export function wrapForMultiplexer(sequence: string): string {
  if (process.env['TMUX']) {
    const escaped = sequence.replaceAll('\x1b', '\x1b\x1b')
    return `\x1bPtmux;${escaped}\x1b\\`
  }
  if (process.env['STY']) {
    return `\x1bP${sequence}\x1b\\`
  }
  return sequence
}

export type ClipboardPath = 'native' | 'tmux-buffer' | 'osc52'

export function getClipboardPath(): ClipboardPath {
  const nativeAvailable =
    process.platform === 'darwin' && !process.env['SSH_CONNECTION']
  if (nativeAvailable) return 'native'
  if (process.env['TMUX']) return 'tmux-buffer'
  return 'osc52'
}

/** Wrap a payload in tmux's DCS passthrough envelope: `ESC P tmux ; <payload> ESC \\`. */
function tmuxPassthrough(payload: string): string {
  return `${ESC}Ptmux;${payload.replaceAll(ESC, ESC + ESC)}${ST}`
}

/** Load text into tmux's paste buffer via `tmux load-buffer`. */
export async function tmuxLoadBuffer(text: string): Promise<boolean> {
  if (!process.env['TMUX']) return false
  const args =
    process.env['LC_TERMINAL'] === 'iTerm2'
      ? ['load-buffer', '-']
      : ['load-buffer', '-w', '-']
  const { code } = await execFileNoThrow('tmux', args, {
    input: text,
    timeout: 2000,
  })
  return code === 0
}

export async function setClipboard(text: string): Promise<string> {
  const b64 = Buffer.from(text, 'utf8').toString('base64')
  const raw = osc(OSC.CLIPBOARD, 'c', b64)

  if (!process.env['SSH_CONNECTION']) copyNative(text)

  const tmuxBufferLoaded = await tmuxLoadBuffer(text)

  if (tmuxBufferLoaded) return tmuxPassthrough(`${ESC}]52;c;${b64}${BEL}`)
  return raw
}

// Linux clipboard tool: undefined = not yet probed, null = none available.
// Probe order: wl-copy (Wayland) → xclip (X11) → xsel (X11 fallback). The
// result is cached so a repeated copy in a long session doesn't fan out
// three subprocesses on every keystroke that triggered a copy.
let linuxCopy: 'wl-copy' | 'xclip' | 'xsel' | null | undefined

function copyNative(text: string): void {
  const opts = { input: text, timeout: 2000 }
  switch (process.platform) {
    case 'darwin':
      void execFileNoThrow('pbcopy', [], opts)
      return
    case 'linux': {
      if (linuxCopy === null) return
      if (linuxCopy === 'wl-copy') {
        void execFileNoThrow('wl-copy', [], opts)
        return
      }
      if (linuxCopy === 'xclip') {
        void execFileNoThrow('xclip', ['-selection', 'clipboard'], opts)
        return
      }
      if (linuxCopy === 'xsel') {
        void execFileNoThrow('xsel', ['--clipboard', '--input'], opts)
        return
      }
      // First call: probe Wayland then X11, cache the first that returns 0.
      void execFileNoThrow('wl-copy', [], opts).then(r => {
        if (r.code === 0) {
          linuxCopy = 'wl-copy'
          return
        }
        void execFileNoThrow('xclip', ['-selection', 'clipboard'], opts).then(
          r2 => {
            if (r2.code === 0) {
              linuxCopy = 'xclip'
              return
            }
            void execFileNoThrow('xsel', ['--clipboard', '--input'], opts).then(
              r3 => {
                linuxCopy = r3.code === 0 ? 'xsel' : null
              },
            )
          },
        )
      })
      return
    }
    case 'win32':
      // clip.exe is always present on Windows. Its Unicode handling uses
      // the system locale encoding, which mangles emoji and CJK — but this
      // is a fallback for when OSC 52 didn't work, so "good enough" wins.
      void execFileNoThrow('clip', [], opts)
      return
  }
}

/** @internal Test-only escape hatch to clear the cached Linux probe result. */
export function _resetLinuxCopyCache(): void {
  linuxCopy = undefined
}

/** OSC command numbers. */
export const OSC = {
  SET_TITLE_AND_ICON: 0,
  SET_ICON: 1,
  SET_TITLE: 2,
  SET_COLOR: 4,
  SET_CWD: 7,
  HYPERLINK: 8,
  ITERM2: 9, // iTerm2 proprietary
  SET_FG_COLOR: 10,
  SET_BG_COLOR: 11,
  SET_CURSOR_COLOR: 12,
  CLIPBOARD: 52,
  KITTY: 99, // kitty notification protocol
  RESET_COLOR: 104,
  RESET_FG_COLOR: 110,
  RESET_BG_COLOR: 111,
  RESET_CURSOR_COLOR: 112,
  SEMANTIC_PROMPT: 133,
  GHOSTTY: 777, // Ghostty notification protocol
  TAB_STATUS: 21337, // tab status extension
} as const

export function parseOSC(content: string): Action | null {
  const semicolonIdx = content.indexOf(';')
  const command = semicolonIdx >= 0 ? content.slice(0, semicolonIdx) : content
  const data = semicolonIdx >= 0 ? content.slice(semicolonIdx + 1) : ''

  const commandNum = parseInt(command, 10)

  if (commandNum === OSC.SET_TITLE_AND_ICON) {
    return { type: 'title', action: { type: 'both', title: data } }
  }
  if (commandNum === OSC.SET_ICON) {
    return { type: 'title', action: { type: 'iconName', name: data } }
  }
  if (commandNum === OSC.SET_TITLE) {
    return { type: 'title', action: { type: 'windowTitle', title: data } }
  }

  // OSC 8 hyperlinks. Empty URL = close sequence (per spec); params is
  // a colon-separated `k=v` list before the second semicolon.
  if (commandNum === OSC.HYPERLINK) {
    const parts = data.split(';')
    const paramsStr = parts[0] ?? ''
    const url = parts.slice(1).join(';')

    if (url === '') {
      return { type: 'link', action: { type: 'end' } }
    }

    const params: Record<string, string> = {}
    if (paramsStr) {
      for (const pair of paramsStr.split(':')) {
        const eqIdx = pair.indexOf('=')
        if (eqIdx >= 0) {
          params[pair.slice(0, eqIdx)] = pair.slice(eqIdx + 1)
        }
      }
    }

    return {
      type: 'link',
      action: {
        type: 'start',
        url,
        params: Object.keys(params).length > 0 ? params : undefined,
      },
    }
  }

  if (commandNum === OSC.TAB_STATUS) {
    return { type: 'tabStatus', action: parseTabStatus(data) }
  }

  return { type: 'unknown', sequence: `\x1b]${content}` }
}

export function parseOscColor(spec: string): Color | null {
  const hex = spec.match(/^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i)
  if (hex) {
    return {
      type: 'rgb',
      r: parseInt(hex[1]!, 16),
      g: parseInt(hex[2]!, 16),
      b: parseInt(hex[3]!, 16),
    }
  }
  const rgb = spec.match(
    /^rgb:([0-9a-f]{1,4})\/([0-9a-f]{1,4})\/([0-9a-f]{1,4})$/i,
  )
  if (rgb) {
    const scale = (s: string): number =>
      Math.round((parseInt(s, 16) / (16 ** s.length - 1)) * 255)
    return {
      type: 'rgb',
      r: scale(rgb[1]!),
      g: scale(rgb[2]!),
      b: scale(rgb[3]!),
    }
  }
  return null
}

/** Parse an OSC 21337 payload of the form `key=value;key=value;…`. */
function parseTabStatus(data: string): TabStatusAction {
  const action: TabStatusAction = {}
  for (const [key, value] of splitTabStatusPairs(data)) {
    switch (key) {
      case 'indicator':
        action.indicator = value === '' ? null : parseOscColor(value)
        break
      case 'status':
        action.status = value === '' ? null : value
        break
      case 'status-color':
        action.statusColor = value === '' ? null : parseOscColor(value)
        break
    }
  }
  return action
}

/** Split a `k=v;k=v` payload honouring `\;` and `\\` escapes inside values. */
function* splitTabStatusPairs(data: string): Generator<[string, string]> {
  let key = ''
  let val = ''
  let inVal = false
  let esc = false
  for (const c of data) {
    if (esc) {
      if (inVal) val += c
      else key += c
      esc = false
    } else if (c === '\\') {
      esc = true
    } else if (c === ';') {
      yield [key, val]
      key = ''
      val = ''
      inVal = false
    } else if (c === '=' && !inVal) {
      inVal = true
    } else if (inVal) {
      val += c
    } else {
      key += c
    }
  }
  if (key || inVal) yield [key, val]
}

// ---------------------------------------------------------------------------
// Output generators
// ---------------------------------------------------------------------------

/** Build an OSC 8 hyperlink start (or end, if `url` is empty). */
export function link(url: string, params?: Record<string, string>): string {
  if (!url) return LINK_END
  const p = { id: oscLinkId(url), ...params }
  const paramStr = Object.entries(p)
    .map(([k, v]) => `${k}=${v}`)
    .join(':')
  return osc(OSC.HYPERLINK, paramStr, url)
}

function oscLinkId(url: string): string {
  let h = 0
  for (let i = 0; i < url.length; i++)
    h = ((h << 5) - h + url.charCodeAt(i)) | 0
  return (h >>> 0).toString(36)
}

/** Close an OSC 8 hyperlink. */
export const LINK_END = osc(OSC.HYPERLINK, '', '')

// ---------------------------------------------------------------------------
// iTerm2 OSC 9 subcommands
// ---------------------------------------------------------------------------

/** iTerm2 OSC 9 subcommand numbers. */
export const ITERM2 = {
  NOTIFY: 0,
  BADGE: 2,
  PROGRESS: 4,
} as const

/** Progress operation codes (used with `ITERM2.PROGRESS`). */
export const PROGRESS = {
  CLEAR: 0,
  SET: 1,
  ERROR: 2,
  INDETERMINATE: 3,
} as const

/** Clear the iTerm2 progress indicator (OSC 9 ; 4 ; 0 ; BEL). */
export const CLEAR_ITERM2_PROGRESS = `${OSC_PREFIX}${OSC.ITERM2};${ITERM2.PROGRESS};${PROGRESS.CLEAR};${BEL}`

/** Clear the terminal window/icon title (OSC 0 with empty payload + BEL). */
export const CLEAR_TERMINAL_TITLE = `${OSC_PREFIX}${OSC.SET_TITLE_AND_ICON};${BEL}`

/** Clear all three OSC 21337 tab-status fields. Used on exit. */
export const CLEAR_TAB_STATUS = osc(
  OSC.TAB_STATUS,
  'indicator=;status=;status-color=',
)

/** Gate for OSC 21337 emission. */
export function supportsTabStatus(): boolean {
  return process.env.USER_TYPE === 'ant'
}

/** Emit an OSC 21337 tab-status sequence. */
export function tabStatus(fields: TabStatusAction): string {
  const parts: string[] = []
  const rgb = (c: Color): string =>
    c.type === 'rgb'
      ? `#${[c.r, c.g, c.b].map(n => n.toString(16).padStart(2, '0')).join('')}`
      : ''
  if ('indicator' in fields)
    parts.push(`indicator=${fields.indicator ? rgb(fields.indicator) : ''}`)
  if ('status' in fields)
    parts.push(
      `status=${fields.status?.replaceAll('\\', '\\\\').replaceAll(';', '\\;') ?? ''}`,
    )
  if ('statusColor' in fields)
    parts.push(
      `status-color=${fields.statusColor ? rgb(fields.statusColor) : ''}`,
    )
  return osc(OSC.TAB_STATUS, parts.join(';'))
}
