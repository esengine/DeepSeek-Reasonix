/**
 * 生成式音乐引擎 — Token 驱动版
 *
 * 每次 AI token 到达时触发一个音符。模型快则音符密，模型慢则音符稀。
 * 信号链：振荡器 → ADSR 包络 → 低通滤波器 → 混响 → 扬声器。
 *
 * 支持 4 种预设音色，通过 localStorage 键 "generativeMusicPreset" 持久化。
 * 设为 "off" 可关闭。
 */

export type GenerativePreset = "off" | "classic" | "ethereal" | "digital" | "retro";

type NonEmptyPreset = "classic" | "ethereal" | "digital" | "retro";

interface PresetConfig {
  oscillatorType: OscillatorType;
  attack: number;          // 秒
  decay: number;           // 秒
  sustain: number;         // 0-1 电平
  release: number;         // 秒
  filterFreq: number;      // Hz
  reverbDecay: number;     // 秒（脉冲响应长度）
  reverbWet: number;       // 0-1 混合比
  masterVolume: number;    // 0-1
}

const PRESETS: Record<NonEmptyPreset, PresetConfig> = {
  classic: {
    oscillatorType: "triangle",
    attack: 0.01,
    decay: 0.2,
    sustain: 0.2,
    release: 0.3,
    filterFreq: 2000,
    reverbDecay: 0.3,
    reverbWet: 0.2,
    masterVolume: 0.08,
  },
  ethereal: {
    oscillatorType: "sine",
    attack: 0.1,
    decay: 0.4,
    sustain: 0.4,
    release: 0.8,
    filterFreq: 3000,
    reverbDecay: 0.8,
    reverbWet: 0.35,
    masterVolume: 0.07,
  },
  digital: {
    oscillatorType: "square",
    attack: 0.005,
    decay: 0.1,
    sustain: 0.1,
    release: 0.1,
    filterFreq: 5000,
    reverbDecay: 0.05,
    reverbWet: 0.05,
    masterVolume: 0.06,
  },
  retro: {
    oscillatorType: "sawtooth",
    attack: 0.02,
    decay: 0.3,
    sustain: 0.3,
    release: 0.4,
    filterFreq: 1500,
    reverbDecay: 0.35,
    reverbWet: 0.25,
    masterVolume: 0.08,
  },
};

// C 大调五声音阶：C D F G A (无半音)
const SCALE_FREQS = [261.63, 293.66, 349.23, 392.0, 440.0, 523.25, 587.33]; // C4–D5
const OCTAVE_FREQS = [130.81, 146.83, 174.61, 196.0, 220.0, 261.63, 293.66]; // C3–D3 (低八度)

const PRESET_KEY = "generativeMusicPreset";

function readPresetPref(): GenerativePreset {
  if (typeof localStorage === "undefined") return "ethereal";
  const val = localStorage.getItem(PRESET_KEY);
  if (val === "off" || val === "classic" || val === "ethereal" || val === "digital" || val === "retro") return val;
  return "ethereal";
}

function writePresetPref(pref: GenerativePreset): void {
  if (typeof localStorage !== "undefined") {
    localStorage.setItem(PRESET_KEY, pref);
  }
}

export function getGenerativePreset(): GenerativePreset {
  return readPresetPref();
}

export function setGenerativePreset(pref: GenerativePreset): void {
  writePresetPref(pref);
}

export function isGenerativeMusicEnabled(): boolean {
  return readPresetPref() !== "off";
}

// ── 引擎 ──────────────────────────────────────────────────────────────────────

class GenerativeMusicEngine {
  private ctx: AudioContext | null = null;
  private masterGain: GainNode | null = null;
  private filterNode: BiquadFilterNode | null = null;
  private reverbNode: ConvolverNode | null = null;
  private reverbGain: GainNode | null = null;      // wet
  private preset: NonEmptyPreset = "ethereal";
  private running = false;
  private lastNoteTime = 0;     // 用于节流，ms 时间戳
  private tokenToggle = 0;     // 每 4 个 token 触发一次

  /** 启动引擎：创建 AudioContext + 信号链。 */
  start(preset?: NonEmptyPreset): void {
    if (this.running) return;
    const p = preset ?? readPresetPref();
    if (p === "off") return;
    this.preset = p;
    this.running = true;

    try {
      this.ctx = new AudioContext();
      this.buildSignalChain();
    } catch (e) {
      console.warn("generative-music: failed to create AudioContext", e);
      this.running = false;
    }
  }

  /** 淡出并停止。 */
  stop(): void {
    if (!this.running) return;
    this.running = false;

    // 主音量淡出
    if (this.masterGain && this.ctx) {
      try {
        const now = this.ctx.currentTime;
        this.masterGain.gain.cancelScheduledValues(now);
        this.masterGain.gain.setValueAtTime(this.masterGain.gain.value, now);
        this.masterGain.gain.linearRampToValueAtTime(0, now + 0.3);
      } catch (e) {
        console.warn("generative-music: gain ramp failed", e);
      }
    }

    // 延迟关闭 AudioContext
    const ctx = this.ctx;
    setTimeout(() => {
      ctx?.close();
    }, 500);
    this.ctx = null;
    this.masterGain = null;
    this.filterNode = null;
    this.reverbNode = null;
    this.reverbGain = null;
  }

  /** 切换预设（下一次 playTokenNote 生效）。 */
  setPreset(preset: NonEmptyPreset): void {
    this.preset = preset;
    writePresetPref(preset);
    if (this.ctx && this.masterGain) {
      this.buildSignalChain();
    }
  }

  get isRunning(): boolean {
    return this.running;
  }

  /**
   * 每次 token 到达时调用一次。双重节流：
   * - 每 4 个 token 采样一次（应对 DeepSeek 高频输出）
   * - 前后间隔 < 100ms 则跳过
   */
  playTokenNote(): void {
    if (!this.running || !this.ctx) return;

    // 每 4 个 token 触发一次（DeepSeek 生成太快）
    this.tokenToggle++;
    if (this.tokenToggle % 4 !== 0) return;

    const now = performance.now();
    if (now - this.lastNoteTime < 100) return;
    this.lastNoteTime = now;

    const time = this.ctx.currentTime;

    // 从 C 大调五声音阶中均匀随机挑一个音高
    const useLow = Math.random() < 0.15;
    const freqs = useLow ? OCTAVE_FREQS : SCALE_FREQS;
    const freq = freqs[Math.floor(Math.random() * freqs.length)];

    // 随机微调 ±5%
    const detune = 1 + (Math.random() - 0.5) * 0.1;
    this.playNote(time, freq * detune);
  }

  // ── 信号链构建 ────────────────────────────────────────────────────────────

  private buildSignalChain(): void {
    if (!this.ctx) return;
    const cfg = PRESETS[this.preset];

    // 主音量
    if (this.masterGain) this.masterGain.disconnect();
    this.masterGain = this.ctx.createGain();
    this.masterGain.gain.value = cfg.masterVolume;
    this.masterGain.connect(this.ctx.destination);

    // 低通滤波器
    if (this.filterNode) this.filterNode.disconnect();
    this.filterNode = this.ctx.createBiquadFilter();
    this.filterNode.type = "lowpass";
    this.filterNode.frequency.value = cfg.filterFreq;
    this.filterNode.Q.value = 1;
    this.filterNode.connect(this.masterGain);

    // 混响：filter → reverb → reverbGain → master
    if (this.reverbNode) this.reverbNode.disconnect();
    if (this.reverbGain) this.reverbGain.disconnect();

    this.reverbNode = this.ctx.createConvolver();
    this.reverbNode.buffer = this.createReverbImpulse(this.ctx, cfg.reverbDecay);
    this.reverbGain = this.ctx.createGain();
    this.reverbGain.gain.value = cfg.reverbWet;
    this.filterNode.connect(this.reverbNode);
    this.reverbNode.connect(this.reverbGain);
    this.reverbGain.connect(this.masterGain);
  }

  private createReverbImpulse(ctx: AudioContext, duration: number): AudioBuffer {
    const sampleRate = ctx.sampleRate;
    const length = Math.max(Math.round(sampleRate * duration), 1);
    const buffer = ctx.createBuffer(2, length, sampleRate);
    for (let ch = 0; ch < 2; ch++) {
      const data = buffer.getChannelData(ch);
      for (let i = 0; i < length; i++) {
        const t = i / length;
        data[i] = (Math.random() * 2 - 1) * Math.pow(1 - t, 1.5) * (1 + Math.sin(t * Math.PI * 3) * 0.3);
      }
    }
    return buffer;
  }

  // ── 音符播放 ──────────────────────────────────────────────────────────────

  private playNote(time: number, freq: number): void {
    if (!this.ctx) return;
    const cfg = PRESETS[this.preset];
    const now = this.ctx.currentTime;
    const t = Math.max(time, now + 0.005);

    // 振荡器
    const osc = this.ctx.createOscillator();
    osc.type = cfg.oscillatorType;
    osc.frequency.setValueAtTime(freq, t);

    // ADSR 包络
    const envGain = this.ctx.createGain();
    const env = envGain.gain;
    const noteEnd = t + cfg.attack + cfg.decay + cfg.release + 0.01;
    env.setValueAtTime(0, t);
    env.linearRampToValueAtTime(1, t + cfg.attack);
    env.linearRampToValueAtTime(cfg.sustain, t + cfg.attack + cfg.decay);
    env.setValueAtTime(cfg.sustain, t + cfg.attack + cfg.decay + 0.01);
    env.linearRampToValueAtTime(0.001, t + cfg.attack + cfg.decay + 0.01 + cfg.release);

    // osc → env → filter (已连到 master)
    osc.connect(envGain);
    if (this.filterNode) {
      envGain.connect(this.filterNode);
    } else {
      envGain.connect(this.ctx.destination);
    }

    osc.start(t);
    osc.stop(noteEnd);
  }

  /** 生成快速预览音（设置面板用）。 */
  playPreview(preset: NonEmptyPreset): void {
    const ctx = new AudioContext();
    const cfg = PRESETS[preset];

    const filter = ctx.createBiquadFilter();
    filter.type = "lowpass";
    filter.frequency.value = cfg.filterFreq;

    const reverb = ctx.createConvolver();
    reverb.buffer = this.createReverbImpulse(ctx, cfg.reverbDecay);
    const reverbGain = ctx.createGain();
    reverbGain.gain.value = cfg.reverbWet;

    const master = ctx.createGain();
    master.gain.value = cfg.masterVolume * 2;
    master.connect(ctx.destination);

    filter.connect(master);
    filter.connect(reverb);
    reverb.connect(reverbGain);
    reverbGain.connect(master);

    // 快速上行 4 个音展示音色
    const notes = [SCALE_FREQS[0], SCALE_FREQS[2], SCALE_FREQS[4], SCALE_FREQS[6]];
    notes.forEach((freq, i) => {
      const t = ctx.currentTime + i * 0.05;

      const osc = ctx.createOscillator();
      osc.type = cfg.oscillatorType;
      osc.frequency.setValueAtTime(freq, t);

      const env = ctx.createGain();
      env.gain.setValueAtTime(0, t);
      env.gain.linearRampToValueAtTime(1, t + cfg.attack);
      env.gain.linearRampToValueAtTime(cfg.sustain, t + cfg.attack + cfg.decay);
      env.gain.setValueAtTime(cfg.sustain, t + 0.05);
      env.gain.linearRampToValueAtTime(0.001, t + cfg.attack + cfg.decay + cfg.release);

      osc.connect(env);
      env.connect(filter);
      osc.start(t);
      osc.stop(t + cfg.attack + cfg.decay + cfg.release + 0.05);
    });

    setTimeout(() => ctx.close(), 3000);
  }
}

// 单例导出
export const generativeMusic = new GenerativeMusicEngine();
