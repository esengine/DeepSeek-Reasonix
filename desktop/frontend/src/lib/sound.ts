/**
 * 通知音效系统
 *
 * 支持合成音效和 WAV 文件播放两种模式，默认关闭。
 * 两个场景的偏好分别存入 localStorage:
 *   notificationSoundSuccess  —— 生成完成
 *   notificationSoundAttention —— AI 提问
 *   notificationSoundVolume —— 统一通知音量（0–100）
 *   值："off" | "synth" | "positive" | "correct" | "start" | "back"
 */

export type SoundWavPref = "off" | "synth" | "positive" | "correct" | "start" | "back";

const SUCCESS_KEY = "notificationSoundSuccess";
const ATTENTION_KEY = "notificationSoundAttention";
export const NOTIFICATION_VOLUME_STORAGE_KEY = "notificationSoundVolume";
export const NOTIFICATION_VOLUME_MIN = 0;
export const NOTIFICATION_VOLUME_MAX = 100;
export const DEFAULT_NOTIFICATION_VOLUME = 70;

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

export function normalizeNotificationVolume(value: unknown): number {
  const raw = typeof value === "string" ? value.trim() : value;
  if (raw === "" || raw === null || raw === undefined) return DEFAULT_NOTIFICATION_VOLUME;
  const numeric = Number(raw);
  if (!Number.isFinite(numeric)) return DEFAULT_NOTIFICATION_VOLUME;
  return Math.min(NOTIFICATION_VOLUME_MAX, Math.max(NOTIFICATION_VOLUME_MIN, Math.round(numeric)));
}

export function getNotificationVolume(): number {
  if (typeof localStorage === "undefined") return DEFAULT_NOTIFICATION_VOLUME;
  try {
    const value = localStorage.getItem(NOTIFICATION_VOLUME_STORAGE_KEY);
    return value === null ? DEFAULT_NOTIFICATION_VOLUME : normalizeNotificationVolume(value);
  } catch {
    return DEFAULT_NOTIFICATION_VOLUME;
  }
}

export function setNotificationVolume(volume: number): number {
  const normalized = normalizeNotificationVolume(volume);
  try {
    if (typeof localStorage !== "undefined") {
      localStorage.setItem(NOTIFICATION_VOLUME_STORAGE_KEY, String(normalized));
    }
  } catch {
    // Private browsing and locked-down WebViews may reject localStorage writes.
  }
  return normalized;
}

export function notificationVolumeToGain(volume: unknown): number {
  return normalizeNotificationVolume(volume) / NOTIFICATION_VOLUME_MAX;
}

type WavSoundPref = Exclude<SoundWavPref, "off" | "synth">;

// The bundled WAV files differ by up to 4.1 LUFS. These trims normalize them
// to the quietest source (-18.9 LUFS) without boosting any asset above its
// recorded peak. The master volume is applied after the source trim.
const WAV_LOUDNESS_TRIM: Record<WavSoundPref, number> = {
  positive: 0.62,
  correct: 0.85,
  start: 1,
  back: 0.70,
};

export function notificationWavGain(pref: WavSoundPref, outputVolume: number): number {
  const safeVolume = Number.isFinite(outputVolume)
    ? Math.min(1, Math.max(0, outputVolume))
    : 0;
  return safeVolume * WAV_LOUDNESS_TRIM[pref];
}

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

function playSynthSuccess(ctx: AudioContext, outputVolume: number): void {
  playSynthNote(ctx, ctx.destination, 1318.5, 0, 0.20, outputVolume * 0.35);
  playSynthNote(ctx, ctx.destination, 1568.0, 0.07, 0.22, outputVolume * 0.30);
  playSynthNote(ctx, ctx.destination, 2093.0, 0.14, 0.30, outputVolume * 0.24);
}

function playSynthAttention(ctx: AudioContext, outputVolume: number): void {
  playSynthNote(ctx, ctx.destination, 1760.0, 0, 0.14, outputVolume * 0.40);
  playSynthNote(ctx, ctx.destination, 1318.5, 0.09, 0.22, outputVolume * 0.34);
}

// ── Play helpers ─────────────────────────────────────────────────────────────

async function playWav(pref: WavSoundPref, volume: number, fallback: (ctx: AudioContext, outputVolume: number) => void): Promise<void> {
  const url = soundFilePath(pref);
  if (!url) return;
  const ctx = new AudioContext();
  try {
    const buf = await loadBuffer(ctx, url);
    if (buf) {
      playBuffer(ctx, buf, notificationWavGain(pref, volume));
    } else {
      fallback(ctx, volume);
    }
  } catch {
    fallback(ctx, volume);
  }
  setTimeout(() => ctx.close(), 2000);
}

// ── Public API ───────────────────────────────────────────────────────────────

export function playSuccessChime(): void {
  const pref = getSuccessPreference();
  if (pref === "off") return;
  const volume = notificationVolumeToGain(getNotificationVolume());
  if (volume <= 0) return;
  if (pref === "synth") {
    try {
      const ctx = new AudioContext();
      playSynthSuccess(ctx, volume);
      setTimeout(() => ctx.close(), 600);
    } catch { /* silent */ }
  } else {
    void playWav(pref, volume, playSynthSuccess);
  }
}

export function playAttentionChime(): void {
  const pref = getAttentionPreference();
  if (pref === "off") return;
  const volume = notificationVolumeToGain(getNotificationVolume());
  if (volume <= 0) return;
  if (pref === "synth") {
    try {
      const ctx = new AudioContext();
      playSynthAttention(ctx, volume);
      setTimeout(() => ctx.close(), 500);
    } catch { /* silent */ }
  } else {
    void playWav(pref, volume, playSynthAttention);
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
