export interface Point {
  x: number;
  y: number;
}

/** The intro's beats, in milliseconds from its first frame. */
export const BEAT = {
  push: 380,
  hit: 620,
  lock: 860,
  unfold: 980,
  quiet: 1500,
};

const STARS = 240;
const EMBERS = 18;

interface Star {
  x: number;
  y: number;
  r: number;
  a: number;
  tw: number;
}

interface Ember {
  x: number;
  y: number;
  vx: number;
  vy: number;
  life: number;
}

const clamp = (v: number) => (v < 0 ? 0 : v > 1 ? 1 : v);
const lerp = (a: number, b: number, u: number) => a + (b - a) * u;

/** Draws the sky for time t: the stars, the meteor and the light of its
 *  impact. The letter it lands as is the DOM's, driven by intro.ts. */
export class Sky {
  private stars: Star[] = [];
  private embers: Ember[] = [];
  private last = 0;
  private readonly from: Point;
  private readonly bend: Point;
  private readonly dir: Point;

  constructor(
    private readonly ctx: CanvasRenderingContext2D,
    private readonly w: number,
    private readonly h: number,
    private readonly impact: Point,
  ) {
    this.from = { x: w * 1.02, y: h * 0.1 };
    this.bend = { x: lerp(this.from.x, impact.x, 0.62), y: lerp(this.from.y, impact.y, 0.5) };
    const dx = this.bend.x - this.from.x;
    const dy = this.bend.y - this.from.y;
    const n = Math.hypot(dx, dy) || 1;
    this.dir = { x: dx / n, y: dy / n };
    for (let i = 0; i < STARS; i++) {
      this.stars.push({
        x: Math.random() * w,
        y: Math.random() * h,
        r: Math.random() < 0.1 ? 1.4 : 0.6 + Math.random() * 0.5,
        a: 0.3 + Math.random() * 0.55,
        tw: Math.random() * Math.PI * 2,
      });
    }
  }

  frame(t: number) {
    const { ctx, w, h } = this;
    const dt = Math.min(40, Math.max(0, t - this.last));
    this.last = t;
    ctx.globalCompositeOperation = "source-over";
    ctx.clearRect(0, 0, w, h);
    ctx.globalCompositeOperation = "lighter";
    this.drawStars(t);
    if (t < BEAT.hit) this.drawMeteor(t);
    else this.drawImpact(t, dt);
  }

  private zoom(t: number) {
    if (t < BEAT.push || t >= BEAT.hit) return 1;
    return 1 + 5 * clamp((t - BEAT.push) / (BEAT.hit - BEAT.push)) ** 2.4;
  }

  private drawStars(t: number) {
    const { ctx, impact } = this;
    const z = this.zoom(t);
    const zPrev = this.zoom(t - 18);
    const dim = t >= BEAT.hit ? 0.45 + 0.2 * clamp((t - BEAT.hit) / 800) : 1;
    ctx.lineCap = "round";
    for (const s of this.stars) {
      const a = s.a * dim * (0.8 + 0.2 * Math.sin(t / 420 + s.tw));
      const x = impact.x + (s.x - impact.x) * z;
      const y = impact.y + (s.y - impact.y) * z;
      if (z > 1.03) {
        const px = impact.x + (s.x - impact.x) * zPrev;
        const py = impact.y + (s.y - impact.y) * zPrev;
        ctx.strokeStyle = `rgba(236,228,214,${a})`;
        ctx.lineWidth = s.r;
        ctx.beginPath();
        ctx.moveTo(px, py);
        ctx.lineTo(x, y);
        ctx.stroke();
      } else {
        ctx.fillStyle = `rgba(226,224,240,${a})`;
        ctx.beginPath();
        ctx.arc(x, y, s.r / 2, 0, Math.PI * 2);
        ctx.fill();
      }
    }
  }

  private head(t: number) {
    if (t < BEAT.push) {
      const u = t / BEAT.push;
      return { x: lerp(this.from.x, this.bend.x, u), y: lerp(this.from.y, this.bend.y, u), e: 0 };
    }
    const e = clamp((t - BEAT.push) / (BEAT.hit - BEAT.push)) ** 2.4;
    return { x: lerp(this.bend.x, this.impact.x, e), y: lerp(this.bend.y, this.impact.y, e), e };
  }

  private drawMeteor(t: number) {
    const { ctx, dir, w } = this;
    const p = this.head(t);
    const core = 1.3 + 9 * p.e;
    const len = w * 0.3 * Math.min(1, t / 120) * (1 - 0.55 * p.e);
    const ex = p.x - dir.x * len;
    const ey = p.y - dir.y * len;
    const nx = -dir.y;
    const ny = dir.x;
    for (const [spread, alpha] of [
      [4, 0.16],
      [1, 1],
    ] as const) {
      const half = core * 0.8 * spread;
      const g = ctx.createLinearGradient(p.x, p.y, ex, ey);
      g.addColorStop(0, `rgba(255,248,230,${alpha})`);
      g.addColorStop(0.1, `rgba(255,214,130,${0.8 * alpha})`);
      g.addColorStop(0.45, `rgba(214,140,48,${0.28 * alpha})`);
      g.addColorStop(1, "rgba(180,100,30,0)");
      ctx.fillStyle = g;
      ctx.beginPath();
      ctx.moveTo(p.x + nx * half, p.y + ny * half);
      ctx.lineTo(ex, ey);
      ctx.lineTo(p.x - nx * half, p.y - ny * half);
      ctx.closePath();
      ctx.fill();
    }
    const halo = core * 5;
    const glow = ctx.createRadialGradient(p.x, p.y, 0, p.x, p.y, halo);
    glow.addColorStop(0, "rgba(255,255,250,1)");
    glow.addColorStop(0.2, "rgba(255,228,170,.7)");
    glow.addColorStop(1, "rgba(240,170,70,0)");
    ctx.fillStyle = glow;
    ctx.beginPath();
    ctx.arc(p.x, p.y, halo, 0, Math.PI * 2);
    ctx.fill();
  }

  private drawImpact(t: number, dt: number) {
    const { ctx, impact, w, h } = this;
    const u = t - BEAT.hit;
    if (u - dt <= 0) this.spawnEmbers();

    const bloom = clamp(u / 420);
    if (bloom < 1) {
      const r = Math.min(w, h) * (0.22 + 0.12 * bloom);
      const g = ctx.createRadialGradient(impact.x, impact.y, 0, impact.x, impact.y, r);
      const a = 0.5 * (1 - bloom) ** 2;
      g.addColorStop(0, `rgba(255,220,160,${a})`);
      g.addColorStop(0.2, `rgba(255,208,140,${a * 0.5})`);
      g.addColorStop(0.5, `rgba(255,196,120,${a * 0.14})`);
      g.addColorStop(1, "rgba(255,190,110,0)");
      ctx.fillStyle = g;
      ctx.fillRect(impact.x - r, impact.y - r, r * 2, r * 2);
    }

    const streak = clamp(u / 620);
    if (streak < 1) {
      const reach = w * (0.28 + 0.2 * streak);
      ctx.save();
      ctx.translate(impact.x, impact.y);
      ctx.scale(1, 0.014);
      const g = ctx.createRadialGradient(0, 0, 0, 0, 0, reach);
      g.addColorStop(0, `rgba(255,236,200,${0.9 * (1 - streak) ** 1.6})`);
      g.addColorStop(0.35, `rgba(255,196,110,${0.35 * (1 - streak) ** 1.6})`);
      g.addColorStop(1, "rgba(255,180,90,0)");
      ctx.fillStyle = g;
      ctx.beginPath();
      ctx.arc(0, 0, reach, 0, Math.PI * 2);
      ctx.fill();
      ctx.restore();
    }

    ctx.lineCap = "round";
    this.embers = this.embers.filter((e) => e.life > 0);
    for (const e of this.embers) {
      e.x += e.vx * dt;
      e.y += e.vy * dt;
      e.vy += 0.0011 * dt;
      e.vx *= 0.985;
      e.life -= dt;
      const a = 0.8 * clamp(e.life / 500);
      ctx.strokeStyle = `rgba(255,${170 + 60 * a},${90 + 60 * a},${a})`;
      ctx.lineWidth = 1.1;
      ctx.beginPath();
      ctx.moveTo(e.x, e.y);
      ctx.lineTo(e.x - e.vx * 12, e.y - e.vy * 12);
      ctx.stroke();
    }
  }

  private spawnEmbers() {
    const { impact } = this;
    for (let i = 0; i < EMBERS; i++) {
      const ang = -Math.PI / 2 + (Math.random() - 0.5) * Math.PI * 1.5;
      const speed = 0.25 + Math.random() * 0.55;
      this.embers.push({
        x: impact.x + (Math.random() - 0.5) * 30,
        y: impact.y + (Math.random() - 0.5) * 20,
        vx: Math.cos(ang) * speed,
        vy: Math.sin(ang) * speed,
        life: 450 + Math.random() * 550,
      });
    }
  }
}
