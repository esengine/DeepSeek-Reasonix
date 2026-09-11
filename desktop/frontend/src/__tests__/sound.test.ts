// Run: tsx src/__tests__/sound.test.ts

import {
  DEFAULT_NOTIFICATION_VOLUME,
  NOTIFICATION_VOLUME_STORAGE_KEY,
  attentionChimeEventKey,
  clearAttentionChimeKeys,
  getNotificationVolume,
  notificationWavGain,
  notificationVolumeToGain,
  normalizeNotificationVolume,
  playAttentionChime,
  playSuccessChime,
  setAttentionPreference,
  setNotificationVolume,
  setSuccessPreference,
  shouldPlayAttentionChimeForEvent,
} from "../lib/sound";

let passed = 0;
let failed = 0;

function eq(a: unknown, b: unknown, label: string) {
  if (a === b) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

console.log("\nsound notifications");

const storedValues = new Map<string, string>();
Object.defineProperty(globalThis, "localStorage", {
  configurable: true,
  value: {
    getItem(key: string) { return storedValues.get(key) ?? null; },
    setItem(key: string, value: string) { storedValues.set(key, value); },
    removeItem(key: string) { storedValues.delete(key); },
    clear() { storedValues.clear(); },
  },
});

{
  eq(getNotificationVolume(), DEFAULT_NOTIFICATION_VOLUME, "legacy installs without a saved volume upgrade to the 70% default");
  eq(setNotificationVolume(85), 85, "notification volume saves a user-selected percentage");
  eq(storedValues.get(NOTIFICATION_VOLUME_STORAGE_KEY), "85", "notification volume persists in its dedicated storage key");
  eq(getNotificationVolume(), 85, "saved notification volume round-trips");
  eq(setNotificationVolume(140), 100, "notification volume clamps values above 100%");
  eq(setNotificationVolume(-10), 0, "notification volume clamps values below 0%");
  storedValues.set(NOTIFICATION_VOLUME_STORAGE_KEY, "not-a-number");
  eq(getNotificationVolume(), DEFAULT_NOTIFICATION_VOLUME, "invalid persisted volume recovers to the default");
  eq(normalizeNotificationVolume(72.6), 73, "notification volume normalizes fractional values to an integer percentage");
  eq(notificationVolumeToGain(70), 0.7, "notification volume converts percentages to Web Audio gain");
  const normalizedWavGains = [
    notificationWavGain("positive", 1),
    notificationWavGain("correct", 1),
    notificationWavGain("start", 1),
    notificationWavGain("back", 1),
  ];
  eq(normalizedWavGains.join(","), "0.62,0.85,1,0.7", "bundled WAVs use their measured loudness-normalization trims");
  eq(Math.max(...normalizedWavGains), 1, "WAV normalization never boosts a source above its recorded peak");
  eq(notificationWavGain("start", 0.7), 0.7, "the master volume scales a normalized WAV after its source trim");
}

{
  const synthPeaks: number[] = [];
  class FakeAudioContext {
    destination = {};
    createOscillator() {
      return {
        type: "sine",
        frequency: { setValueAtTime() {} },
        connect() {},
        start() {},
        stop() {},
      };
    }
    createGain() {
      return {
        gain: {
          setValueAtTime() {},
          linearRampToValueAtTime(value: number) { synthPeaks.push(value); },
          exponentialRampToValueAtTime() {},
        },
        connect() {},
      };
    }
    close() {}
  }
  Object.defineProperty(globalThis, "AudioContext", { configurable: true, value: FakeAudioContext });
  const maxPeak = () => Math.round(Math.max(...synthPeaks) * 1000) / 1000;

  setSuccessPreference("synth");
  setAttentionPreference("synth");
  setNotificationVolume(DEFAULT_NOTIFICATION_VOLUME);
  playSuccessChime();
  eq(maxPeak(), 0.245, "the 70% upgrade default raises the synthesized success peak above the legacy level");
  synthPeaks.length = 0;
  playAttentionChime();
  eq(maxPeak(), 0.28, "the 70% upgrade default keeps attention at least as audible as success");
  synthPeaks.length = 0;
  setNotificationVolume(0);
  playSuccessChime();
  playAttentionChime();
  eq(synthPeaks.length, 0, "0% prevents synthesized notifications from creating audio nodes");
}

{
  eq(attentionChimeEventKey({ kind: "approval_request", tabId: "tab-a", approval: { id: "approval-1" } }), "approval:tab-a:approval-1", "approval request builds a tab-scoped chime key");
  eq(attentionChimeEventKey({ kind: "ask_request", tabId: "tab-a", ask: { id: "ask-1" } }), "ask:tab-a:ask-1", "ask request builds a tab-scoped chime key");
  eq(attentionChimeEventKey({ kind: "approval_request", approval: { id: "approval-1" } }), "approval::approval-1", "legacy approval events without a tab still build a stable key");
  eq(attentionChimeEventKey({ kind: "turn_done" }), undefined, "non-attention events do not build chime keys");
}

{
  const seen = new Set<string>();
  eq(shouldPlayAttentionChimeForEvent({ kind: "approval_request", tabId: "tab-a", approval: { id: "1" } }, seen), true, "first approval event plays");
  eq(shouldPlayAttentionChimeForEvent({ kind: "approval_request", tabId: "tab-a", approval: { id: "1" } }, seen), false, "replayed approval event for the same tab is deduped");
  eq(shouldPlayAttentionChimeForEvent({ kind: "approval_request", tabId: "tab-b", approval: { id: "1" } }, seen), true, "same approval id from another tab still plays");
  eq(shouldPlayAttentionChimeForEvent({ kind: "ask_request", tabId: "tab-a", ask: { id: "1" } }, seen), true, "ask id sharing an approval id still plays");
  eq(shouldPlayAttentionChimeForEvent({ kind: "approval_request" }, seen), false, "malformed approval event does not play");
}

{
  // The dedupe set stays bounded: after many unique prompts it self-prunes
  // while still deduping recently seen ids.
  const seen = new Set<string>();
  for (let i = 0; i < 2000; i++) {
    shouldPlayAttentionChimeForEvent({ kind: "approval_request", tabId: "t", approval: { id: String(i) } }, seen);
  }
  eq(seen.size <= 1024, true, "dedupe set stays bounded after 2000 unique prompts");
  eq(shouldPlayAttentionChimeForEvent({ kind: "approval_request", tabId: "t", approval: { id: "1999" } }, seen), false, "most recent prompt id is still deduped after pruning");
}

{
  // Runtime rebuild clears dedupe keys so reissued ids chime again.
  const seen = new Set<string>();
  shouldPlayAttentionChimeForEvent({ kind: "approval_request", tabId: "tab-a", approval: { id: "1" } }, seen);
  shouldPlayAttentionChimeForEvent({ kind: "ask_request", tabId: "tab-b", ask: { id: "1" } }, seen);
  clearAttentionChimeKeys(seen, "tab-a");
  eq(shouldPlayAttentionChimeForEvent({ kind: "approval_request", tabId: "tab-a", approval: { id: "1" } }, seen), true, "rebuilt tab's reissued approval id chimes again");
  eq(shouldPlayAttentionChimeForEvent({ kind: "ask_request", tabId: "tab-b", ask: { id: "1" } }, seen), false, "other tab's keys survive a scoped clear");
  clearAttentionChimeKeys(seen);
  eq(shouldPlayAttentionChimeForEvent({ kind: "ask_request", tabId: "tab-b", ask: { id: "1" } }, seen), true, "tab-less ready clears every key");
}

if (failed) {
  console.error(`sound notifications: ${failed} failed, ${passed} passed`);
  process.exit(1);
}

console.log(`sound notifications: ${passed} passed`);
