import { useEffect, useRef, useState } from "react";

const SPLASH_FLAG = "reasonix.splash.shown";

export function shouldShowSplash(): boolean {
  try {
    return sessionStorage.getItem(SPLASH_FLAG) !== "1";
  } catch {
    return true;
  }
}

function markSplashShown() {
  try {
    sessionStorage.setItem(SPLASH_FLAG, "1");
  } catch {
    /* sessionStorage unavailable */
  }
}

type Layers = {
  plankton: { x: number; y: number; r: number; d: number; dur: number; drift: number; o: number }[];
  bokeh: { x: number; y: number; r: number; d: number; dur: number; drift: number; o: number; hue: number }[];
  bubbles: { x: number; r: number; d: number; dur: number; sway: number }[];
  rays: { x: number; rot: number; d: number; w: number; o: number }[];
  schools: { cx: number; cy: number; d: number; dur: number; dots: { ox: number; oy: number; d: number }[] }[];
};

function makeLayers(): Layers {
  const rand = (a: number, b: number) => a + Math.random() * (b - a);
  return {
    plankton: Array.from({ length: 90 }, () => ({
      x: rand(0, 100),
      y: rand(0, 100),
      r: rand(0.5, 1.6),
      d: rand(0, 5),
      dur: rand(7, 14),
      drift: rand(-30, 30),
      o: rand(0.25, 0.7),
    })),
    bokeh: Array.from({ length: 14 }, () => ({
      x: rand(0, 100),
      y: rand(20, 90),
      r: rand(6, 18),
      d: rand(0, 4),
      dur: rand(12, 22),
      drift: rand(-40, 40),
      o: rand(0.05, 0.18),
      hue: rand(210, 245),
    })),
    bubbles: Array.from({ length: 18 }, () => ({
      x: rand(0, 100),
      r: rand(2, 6),
      d: rand(0, 6),
      dur: rand(5, 9),
      sway: rand(20, 60),
    })),
    rays: Array.from({ length: 5 }, (_, i) => ({
      x: 10 + i * 20 + rand(-4, 4),
      rot: rand(-6, 6),
      d: rand(0, 2),
      w: rand(40, 90),
      o: rand(0.05, 0.12),
    })),
    schools: Array.from({ length: 3 }, (_, i) => ({
      cx: 20 + i * 30,
      cy: 35 + rand(-10, 10),
      d: i * 0.7,
      dur: rand(14, 20),
      dots: Array.from({ length: 12 }, () => ({
        ox: rand(-4, 4),
        oy: rand(-2, 2),
        d: rand(0, 1.5),
      })),
    })),
  };
}

export function Splash({ onDone }: { onDone: () => void }) {
  const [stage, setStage] = useState(0);
  const layersRef = useRef<Layers | null>(null);
  if (!layersRef.current) layersRef.current = makeLayers();
  const L = layersRef.current;

  useEffect(() => {
    const seq = [120, 380, 600, 1500, 700];
    let acc = 0;
    const timers = seq.map((d, i) => {
      acc += d;
      return window.setTimeout(() => {
        if (i === seq.length - 1) {
          markSplashShown();
          onDone();
        } else {
          setStage(i + 1);
        }
      }, acc);
    });
    return () => {
      for (const t of timers) window.clearTimeout(t);
    };
  }, [onDone]);

  useEffect(() => {
    const skip = (e: KeyboardEvent) => {
      if (e.key !== "Escape" && e.key !== "Enter" && e.key !== " ") return;
      markSplashShown();
      onDone();
    };
    window.addEventListener("keydown", skip);
    return () => window.removeEventListener("keydown", skip);
  }, [onDone]);

  const skipClick = () => {
    markSplashShown();
    onDone();
  };

  return (
    <div className="splash" data-stage={stage} data-fade={stage >= 4} onClick={skipClick}>
      <div className="splash-vignette" />
      <div className="splash-grain" />

      <div className="splash-rays">
        {L.rays.map((r, i) => (
          <span
            key={i}
            className="ray"
            style={{
              left: `${r.x}%`,
              width: `${r.w}px`,
              ["--rot" as never]: `${r.rot}deg`,
              ["--o" as never]: r.o,
              animationDelay: `${r.d}s`,
            }}
          />
        ))}
      </div>

      <div className="splash-bokeh">
        {L.bokeh.map((b, i) => (
          <span
            key={i}
            className="bk"
            style={{
              left: `${b.x}%`,
              bottom: `${b.y}%`,
              width: `${b.r}px`,
              height: `${b.r}px`,
              background: `radial-gradient(circle, oklch(82% 0.14 ${b.hue} / 0.9), oklch(60% 0.10 ${b.hue} / 0))`,
              ["--o" as never]: b.o,
              ["--dur" as never]: `${b.dur}s`,
              ["--drift" as never]: `${b.drift}px`,
              animationDelay: `${b.d}s`,
            }}
          />
        ))}
      </div>

      <div className="splash-schools">
        {L.schools.map((s, i) => (
          <div
            key={i}
            className="school"
            style={{
              left: `${s.cx}%`,
              top: `${s.cy}%`,
              ["--dur" as never]: `${s.dur}s`,
              animationDelay: `${s.d}s`,
            }}
          >
            {s.dots.map((d, j) => (
              <span
                key={j}
                className="sd"
                style={{
                  left: `${d.ox * 4}px`,
                  top: `${d.oy * 4}px`,
                  animationDelay: `${d.d}s`,
                }}
              />
            ))}
          </div>
        ))}
      </div>

      <div className="splash-plankton">
        {L.plankton.map((p, i) => (
          <span
            key={i}
            className="pk"
            style={{
              left: `${p.x}%`,
              bottom: `${-(p.y / 2)}%`,
              width: `${p.r}px`,
              height: `${p.r}px`,
              ["--o" as never]: p.o,
              ["--dur" as never]: `${p.dur}s`,
              ["--drift" as never]: `${p.drift}px`,
              animationDelay: `${p.d}s`,
            }}
          />
        ))}
      </div>

      <div className="splash-bubbles">
        {L.bubbles.map((b, i) => (
          <span
            key={i}
            className="bb"
            style={{
              left: `${b.x}%`,
              width: `${b.r}px`,
              height: `${b.r}px`,
              ["--dur" as never]: `${b.dur}s`,
              ["--sway" as never]: `${b.sway}px`,
              animationDelay: `${b.d}s`,
            }}
          />
        ))}
      </div>

      <svg className="splash-whale" viewBox="0 0 1200 200" preserveAspectRatio="none">
        <defs>
          <linearGradient id="whaleSil" x1="0" y1="0" x2="1" y2="0">
            <stop offset="0%" stopColor="oklch(78% 0.12 240)" stopOpacity="0" />
            <stop offset="40%" stopColor="oklch(78% 0.12 240)" stopOpacity="0.18" />
            <stop offset="70%" stopColor="oklch(78% 0.12 240)" stopOpacity="0.10" />
            <stop offset="100%" stopColor="oklch(78% 0.12 240)" stopOpacity="0" />
          </linearGradient>
        </defs>
        <path
          d="M 60 120 C 200 70, 420 60, 680 95 C 820 115, 920 130, 1010 122 C 1040 118, 1075 92, 1095 70 C 1095 95, 1080 122, 1055 138 C 990 155, 820 168, 660 158 C 420 142, 220 152, 60 138 Z"
          fill="url(#whaleSil)"
        />
      </svg>

      <div className="splash-stage">
        <div className="splash-line" />
        <h1 className="splash-wordmark">
          <span className="rsx">Reasonix</span>
        </h1>
        <div className="splash-tag">
          <span className="dot" />
          <span>DEEPSEEK&nbsp;·&nbsp;AGENTS</span>
          <span className="dot" />
        </div>
      </div>
    </div>
  );
}
