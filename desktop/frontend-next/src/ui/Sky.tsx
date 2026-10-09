import { useEffect, useRef } from "react";
import { onPresence, present } from "./presence";

// Where each puff sits, how big it is, and how fast it drifts. Three bands
// rather than one spread: parallax is what makes a flat blur read as depth,
// and it only reads if the near band is several times the far one.
const BANDS = [
  { at: 0.08, r: 46, a: 0.16, v: 0.045 },
  { at: 0.34, r: 62, a: 0.22, v: 0.026 },
  { at: 0.6, r: 84, a: 0.3, v: 0.013 },
];
const PUFFS = 34;
// The buffer is small on purpose: it is blurred to 17px and scaled past the
// viewport, so drawing it at window size would cost pixels nobody can see.
const W = 400;
const H = 225;
const FRAME = 1000 / 15;

interface Puff {
  x: number;
  y: number;
  r: number;
  a: number;
  v: number;
  lit: boolean;
}

function seed(): Puff[] {
  const out: Puff[] = [];
  for (let i = 0; i < PUFFS; i++) {
    const b = BANDS[Math.min(BANDS.length - 1, Math.floor((i / PUFFS) * BANDS.length))];
    out.push({
      x: Math.random() * (W + 200) - 100,
      y: b.at * H + Math.random() * H * 0.32,
      r: b.r * (0.65 + Math.random() * 0.7),
      a: b.a * (0.6 + Math.random() * 0.8),
      v: b.v * (0.7 + Math.random() * 0.6),
      // A third of them catch the light. Any more and the whole bank turns
      // gold, which is a sunset rather than a morning.
      lit: Math.random() < 0.3,
    });
  }
  return out;
}

const rgb = (v: string, fallback: string) => (/^\d+\s*,\s*\d+\s*,\s*\d+$/.test(v.trim()) ? v.trim() : fallback);

/** The theme's live backdrop: a drifting cloud bank under a low sun, with a
 *  few light shafts over it. It is inert unless the active pack asked for one
 *  (`data-sky`), and it stops moving entirely under reduced motion — the point
 *  is depth behind a window, never something that pulls the eye off the text. */
export function Sky() {
  const cv = useRef<HTMLCanvasElement>(null);

  useEffect(() => {
    const el = cv.current;
    const ctx = el?.getContext("2d");
    if (!el || !ctx) return;
    el.width = W;
    el.height = H;
    const puffs = seed();
    const still = matchMedia("(prefers-reduced-motion: reduce)");

    let hi = "206,224,255";
    let lit = "216,166,88";
    const readTheme = () => {
      const cs = getComputedStyle(document.documentElement);
      hi = rgb(cs.getPropertyValue("--cloud-hi"), hi);
      lit = rgb(cs.getPropertyValue("--cloud-gilt"), lit);
    };
    readTheme();

    const paint = (move: boolean, step = 1) => {
      ctx.clearRect(0, 0, W, H);
      for (const p of puffs) {
        if (move) {
          p.x += p.v * step;
          if (p.x - p.r > W + 100) p.x = -p.r - 100;
        }
        const g = ctx.createRadialGradient(p.x, p.y, 0, p.x, p.y, p.r);
        g.addColorStop(0, `rgba(${p.lit ? lit : hi},${p.a})`);
        g.addColorStop(1, `rgba(${p.lit ? lit : hi},0)`);
        ctx.fillStyle = g;
        ctx.beginPath();
        ctx.arc(p.x, p.y, p.r, 0, Math.PI * 2);
        ctx.fill();
      }
    };

    let raf = 0;
    let timer = 0;
    let last = 0;
    // Waiting between frames on a timer, not on a rAF loop: a loop that does
    // nothing on 3 of 4 callbacks still wakes the compositor on every one.
    const frame = (now: number) => {
      raf = 0;
      paint(true, Math.min(6, (now - last) / (1000 / 60)));
      last = now;
      timer = window.setTimeout(wake, FRAME);
    };
    const wake = () => {
      timer = 0;
      raf = requestAnimationFrame(frame);
    };
    const halt = () => {
      if (raf) cancelAnimationFrame(raf);
      if (timer) clearTimeout(timer);
      raf = timer = 0;
    };
    const sync = () => {
      if (still.matches || !present()) {
        halt();
        paint(false);
        return;
      }
      if (!raf && !timer) {
        last = performance.now();
        wake();
      }
    };
    const theme = new MutationObserver(() => {
      readTheme();
      if (still.matches || !present()) paint(false);
    });
    theme.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme", "data-pack", "style"] });
    still.addEventListener("change", sync);
    const unwatch = onPresence(sync);
    sync();
    return () => {
      halt();
      still.removeEventListener("change", sync);
      unwatch();
      theme.disconnect();
    };
  }, []);

  return (
    <div className="sky" aria-hidden="true">
      <canvas ref={cv} />
      <div className="sun" />
      <div className="shafts">
        {SHAFTS.map((s, i) => (
          <i key={i} className="shaft" style={s as React.CSSProperties} />
        ))}
      </div>
    </div>
  );
}

// Seven of them, each with its own angle and period so the bank never pulses
// as one thing. Written out rather than generated: a random set redraws
// differently on every mount, and a backdrop that is not the same twice is a
// backdrop somebody notices.
const SHAFTS = [
  { "--x": "8%", "--w": "8%", "--r": "7deg" },
  { "--x": "22%", "--w": "5%", "--r": "5deg" },
  { "--x": "38%", "--w": "11%", "--r": "3deg" },
  { "--x": "54%", "--w": "4%", "--r": "-1deg" },
  { "--x": "64%", "--w": "14%", "--r": "-3deg" },
  { "--x": "81%", "--w": "6%", "--r": "-6deg" },
  { "--x": "91%", "--w": "9%", "--r": "-9deg" },
];
