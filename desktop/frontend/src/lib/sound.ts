/**
 * 通知音效系统
 *
 * 支持合成音效和 WAV 文件播放两种模式，默认关闭。
 * 两个场景的偏好分别存入 localStorage:
 *   notificationSoundSuccess  —— 生成完成
 *   notificationSoundAttention —— AI 提问
 *   值："off" | "synth" | "positive" | "correct" | "start" | "back"
 */

export type SoundWavPref = "off" | "synth" | "positive" | "correct" | "start" | "back";

const SUCCESS_KEY = "notificationSoundSuccess";
const ATTENTION_KEY = "notificationSoundAttention";

function readPref(key: string): SoundWavPref {
  if (typeof localStorage === "undefined") return "off";
  const val = localStorage.getItem(key);
  if (val === "off" || val === "synth" || val === "positive" || val === "correct" || val === "start" || val === "back") return val;
  return "off";
}

function writePref(key: string, pref: SoundWavPref): void {
  if (typeof localStorage !== "undefined") {
    localStorage.setItem(key, pref);
  }
}

export function getSuccessPreference(): SoundWavPref { return readPref(SUCCESS_KEY); }
export function setSuccessPreference(pref: SoundWavPref): void { writePref(SUCCESS_KEY, pref); }
export function getAttentionPreference(): SoundWavPref { return readPref(ATTENTION_KEY); }
export function setAttentionPreference(pref: SoundWavPref): void { writePref(ATTENTION_KEY, pref); }

function soundFilePath(pref: SoundWavPref): string {
  switch (pref) {
    case "positive": return "./sounds/mixkit-positive-notification-951.wav";
    case "correct":  return "./sounds/mixkit-correct-answer-tone-2870.wav";
    case "start":    return "./sounds/mixkit-software-interface-start-2574.wav";
    case "back":     return "./sounds/mixkit-software-interface-back-2575.wav";
    default:         return "";
  }
}

// ── WAV audio cache ──────────────────────────────────────────────────────────
const audioBufferCache = new Map<string, AudioBuffer>();

async function loadBuffer(ctx: AudioContext, url: string): Promise<AudioBuffer | null> {
  const cached = audioBufferCache.get(url);
  if (cached) return cached;
  try {
    const resp = await fetch(url);
    if (!resp.ok) return null;
    const arrayBuffer = await resp.arrayBuffer();
    const decoded = await ctx.decodeAudioData(arrayBuffer);
    audioBufferCache.set(url, decoded);
    return decoded;
  } catch {
    return null;
  }
}

function playBuffer(ctx: AudioContext, buffer: AudioBuffer, volume: number): void {
  const src = ctx.createBufferSource();
  src.buffer = buffer;
  const gain = ctx.createGain();
  gain.gain.value = volume;
  src.connect(gain);
  gain.connect(ctx.destination);
  src.start();
}

// ── Synthesised sounds ───────────────────────────────────────────────────────

function playSynthNote(ctx: AudioContext, dest: AudioNode, freq: number, startTime: number, duration: number, volume: number): void {
  const osc = ctx.createOscillator();
  osc.type = "sine";
  osc.frequency.setValueAtTime(freq, startTime);
  const gain = ctx.createGain();
  gain.gain.setValueAtTime(0, startTime);
  gain.gain.linearRampToValueAtTime(volume, startTime + 0.002);
  gain.gain.exponentialRampToValueAtTime(0.001, startTime + duration);
  osc.connect(gain);
  gain.connect(dest);
  osc.start(startTime);
  osc.stop(startTime + duration);

  const shimmer = ctx.createOscillator();
  shimmer.type = "sine";
  shimmer.frequency.setValueAtTime(freq * 4, startTime);
  const sGain = ctx.createGain();
  sGain.gain.setValueAtTime(0, startTime);
  sGain.gain.linearRampToValueAtTime(volume * 0.12, startTime + 0.002);
  sGain.gain.exponentialRampToValueAtTime(0.001, startTime + duration * 0.6);
  shimmer.connect(sGain);
  sGain.connect(dest);
  shimmer.start(startTime);
  shimmer.stop(startTime + duration);
}

function playSynthSuccess(ctx: AudioContext): void {
  playSynthNote(ctx, ctx.destination, 1318.5, 0, 0.20, 0.12);
  playSynthNote(ctx, ctx.destination, 1568.0, 0.07, 0.22, 0.10);
  playSynthNote(ctx, ctx.destination, 2093.0, 0.14, 0.30, 0.08);
}

function playSynthAttention(ctx: AudioContext): void {
  playSynthNote(ctx, ctx.destination, 1760.0, 0, 0.14, 0.10);
  playSynthNote(ctx, ctx.destination, 1318.5, 0.09, 0.22, 0.08);
}

// ── Play helpers ─────────────────────────────────────────────────────────────

async function playWav(pref: SoundWavPref, volume: number, fallback: (ctx: AudioContext) => void): Promise<void> {
  const url = soundFilePath(pref);
  if (!url) return;
  const ctx = new AudioContext();
  try {
    const buf = await loadBuffer(ctx, url);
    if (buf) {
      playBuffer(ctx, buf, volume);
    } else {
      fallback(ctx);
    }
  } catch {
    fallback(ctx);
  }
  setTimeout(() => ctx.close(), 2000);
}

// ── Public API ───────────────────────────────────────────────────────────────

export function playSuccessChime(): void {
  const pref = getSuccessPreference();
  if (pref === "off") return;
  if (pref === "synth") {
    try {
      const ctx = new AudioContext();
      playSynthSuccess(ctx);
      setTimeout(() => ctx.close(), 600);
    } catch { /* silent */ }
  } else {
    void playWav(pref, 0.35, playSynthSuccess);
  }
}

export function playAttentionChime(): void {
  const pref = getAttentionPreference();
  if (pref === "off") return;
  if (pref === "synth") {
    try {
      const ctx = new AudioContext();
      playSynthAttention(ctx);
      setTimeout(() => ctx.close(), 500);
    } catch { /* silent */ }
  } else {
    void playWav(pref, 0.25, playSynthAttention);
  }
}

export type AttentionChimeEvent = {
  kind?: string;
  tabId?: string;
  approval?: { id?: string };
  ask?: { id?: string };
};

export function attentionChimeEventKey(event: AttentionChimeEvent): string | undefined {
  if (event.kind === "approval_request" && event.approval?.id) return `approval:${event.tabId ?? ""}:${event.approval.id}`;
  if (event.kind === "ask_request" && event.ask?.id) return `ask:${event.tabId ?? ""}:${event.ask.id}`;
  return undefined;
}

// attentionChimeSeenCap bounds the dedupe set. Prompt ids are unique per
// prompt, so the set only ever grows; past the cap the oldest half is dropped
// (insertion order) — replay dedupe only needs to cover recently replayed
// prompts, not the whole session history.
const attentionChimeSeenCap = 512;

// clearAttentionChimeKeys drops dedupe keys after a runtime rebuild. Approval
// and ask ids are per-controller counters starting at "1", so a rebuilt
// controller (model/effort/settings switch) reissues ids an earlier prompt on
// the same tab already used — without this, the first prompt after a rebuild
// is misread as a replay and stays silent. A ready event without a tab id
// (settings rebuilds emit tab-less ready) clears everything: over-clearing
// only re-chimes a replayed pending prompt, which is a desirable reminder,
// while under-clearing mutes a live prompt.
export function clearAttentionChimeKeys(seen: Set<string>, tabId?: string): void {
  if (tabId === undefined || tabId === "") {
    seen.clear();
    return;
  }
  for (const key of [...seen]) {
    if (key.startsWith(`approval:${tabId}:`) || key.startsWith(`ask:${tabId}:`)) {
      seen.delete(key);
    }
  }
}

export function shouldPlayAttentionChimeForEvent(event: AttentionChimeEvent, seen: Set<string>): boolean {
  const key = attentionChimeEventKey(event);
  if (!key || seen.has(key)) return false;
  if (seen.size >= attentionChimeSeenCap) {
    let drop = seen.size - attentionChimeSeenCap / 2;
    for (const k of seen) {
      if (drop-- <= 0) break;
      seen.delete(k);
    }
  }
  seen.add(key);
  return true;
}
