import { BEAT, Sky } from "./sky";
import { shatter } from "./shards";

const EASE = "cubic-bezier(.16, 1, .3, 1)";
const SLAM = "cubic-bezier(.1, .85, .2, 1)";
const LIT = "brightness(2.3) drop-shadow(0 0 10px rgba(255, 204, 128, .9))";
const SLIDE_MS = 650;
const LETTER_MS = 480;
const LETTER_GAP = 45;
const WAIT_MS = 400;
const SLOW_MS = 2500;
const LEAVE_MS = 1600;
const CALM_MS = 300;

export interface Intro {
  /** Resolves once the wordmark stands whole: played out, skipped, or calm. */
  held: Promise<void>;
  /** Lights the first `done` startup steps, each one a thing that has actually
   *  happened: the bundle ran, the kernel answered, the app settled. */
  progress(done: number): void;
  /** Settles the boot screen onto the app's page and resolves when it is gone. */
  leave(): Promise<void>;
}

/** Plays the boot screen: a meteor crosses, the view rushes at it, its impact
 *  slams the R together from shards, and the R unfolds into the wordmark,
 *  which holds under a moving glint for as long as the kernel keeps it waiting. */
export function play(boot: HTMLElement | null): Intro {
  const svg = boot?.querySelector<SVGSVGElement>(".bmark");
  const mid = boot?.querySelector<HTMLElement>(".bmid");
  const letters = boot ? [...boot.querySelectorAll<SVGGraphicsElement>(".bmark .bl")] : [];
  const canvas = boot?.querySelector<HTMLCanvasElement>(".bfx");
  const ctx = canvas?.getContext("2d");
  if (!boot || !svg || !mid || !canvas || !ctx || letters.length < 2) {
    return { held: Promise.resolve(), progress: () => {}, leave: () => Promise.resolve() };
  }
  const calm = matchMedia("(prefers-reduced-motion: reduce)").matches;
  boot.dataset.intro = "";

  const anims: Animation[] = [];
  let cleanup = () => {};
  let release: () => void = () => {};
  const held = new Promise<void>((done) => {
    release = done;
  });
  let wait = 0;
  let slow = 0;
  const hold = () => {
    removeEventListener("pointerdown", skip);
    removeEventListener("keydown", skip);
    removeEventListener("resize", skip);
    // Writes the letters' settled state inline and drops their animations, so
    // the leaving CSS can animate them: a finished script animation outranks it.
    for (const el of letters) el.style.opacity = "1";
    for (const a of anims) {
      const target = (a.effect as KeyframeEffect | null)?.target;
      if (target === svg || letters.includes(target as SVGGraphicsElement)) a.cancel();
    }
    cleanup();
    release();
    wait = window.setTimeout(() => (boot.dataset.wait = ""), WAIT_MS);
    slow = window.setTimeout(() => (boot.dataset.slow = ""), SLOW_MS);
    if (!calm) glint(svg);
  };
  const settle = () => void Promise.all(anims.map((a) => a.finished)).then(hold, hold);

  let raf = 0;
  let started = false;
  let skipped = false;
  function skip() {
    if (skipped) return;
    skipped = true;
    cancelAnimationFrame(raf);
    if (!started) choreograph(0);
    boot!.dataset.skipped = "";
    for (const a of anims) a.finish();
  }

  function choreograph(shift: number) {
    started = true;
    const [r, ...rest] = letters;
    const shards = shatter(svg!, r);
    cleanup = () => shards.layer.remove();
    anims.push(
      ...shards.pieces.map(({ el, dx, dy, turn }) =>
        el.animate(
          [
            { opacity: 0, transform: `translate(${dx}px, ${dy}px) rotate(${turn}deg) scale(1.5)` },
            { opacity: 1, offset: 0.3 },
            { opacity: 1, transform: "none" },
          ],
          { delay: BEAT.hit, duration: BEAT.lock - BEAT.hit, easing: SLAM, fill: "both" },
        ),
      ),
      shards.layer.animate([{ opacity: 1 }, { opacity: 0 }], { delay: BEAT.lock, duration: 40, fill: "forwards" }),
      r.animate([{ opacity: 0 }, { opacity: 1 }], { delay: BEAT.lock - 20, duration: 40, fill: "both" }),
      r.animate([{ filter: LIT }, { filter: "none" }], {
        delay: BEAT.lock - 20,
        duration: 760,
        easing: "ease-out",
        fill: "forwards",
      }),
      mid!.animate([{ transform: "scale(1.045)" }, { transform: "none" }], {
        delay: BEAT.lock - 20,
        duration: 420,
        easing: EASE,
      }),
      svg!.animate([{ transform: `translateX(${shift}px)` }, { transform: "none" }], {
        delay: BEAT.unfold,
        duration: SLIDE_MS,
        easing: EASE,
        fill: "both",
      }),
    );
    // Each letter unrolls from where the one before it stands, so the word
    // ripples out of the R instead of every letter crossing the others.
    const rb = r.getBBox();
    const left = [rb.x + rb.width * 0.5];
    for (const el of rest) {
      const at = Number(el.style.getPropertyValue("--i")) || 1;
      left[at] = Math.min(left[at] ?? Infinity, el.getBBox().x);
    }
    for (const el of rest) {
      const at = Number(el.style.getPropertyValue("--i")) || 1;
      anims.push(
        el.animate(
          [
            { opacity: 0, transform: `translateX(${left[at - 1] - left[at]}px)` },
            { opacity: 1, offset: 0.45 },
            { opacity: 1, transform: "none" },
          ],
          { delay: BEAT.unfold + (at - 1) * LETTER_GAP, duration: LETTER_MS, easing: EASE, fill: "both" },
        ),
      );
    }
    settle();
  }

  if (calm) {
    for (const el of letters) anims.push(el.animate([{ opacity: 0 }, { opacity: 1 }], { duration: CALM_MS, fill: "both" }));
    settle();
  } else {
    const word = svg.getBoundingClientRect();
    const rr = letters[0].getBoundingClientRect();
    const shift = word.left + word.width / 2 - (rr.left + rr.width / 2);
    const impact = { x: rr.left + rr.width / 2 + shift, y: rr.top + rr.height / 2 };
    const w = innerWidth;
    const h = innerHeight;
    const dpr = Math.min(2, devicePixelRatio || 1);
    canvas.width = Math.round(w * dpr);
    canvas.height = Math.round(h * dpr);
    ctx.scale(dpr, dpr);
    const sky = new Sky(ctx, w, h, impact);
    let t0 = 0;
    const tick = (now: number) => {
      if (skipped) return;
      if (!started) {
        choreograph(shift);
        t0 = now;
      }
      const t = now - t0;
      sky.frame(t);
      if (t < BEAT.quiet) raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    addEventListener("pointerdown", skip);
    addEventListener("keydown", skip);
    addEventListener("resize", skip);
  }

  return {
    held,
    progress(done) {
      boot.dataset.step = String(done);
      boot.querySelectorAll(".bsteps i").forEach((mark, i) => {
        mark.toggleAttribute("data-lit", i < done);
        mark.toggleAttribute("data-now", i === done);
      });
    },
    leave() {
      clearTimeout(wait);
      clearTimeout(slow);
      boot.dataset.leave = "";
      return new Promise((done) =>
        setTimeout(() => {
          // The returned Intro outlives the boot screen, and through it this
          // canvas: a detached canvas keeps its window-sized backing and caches.
          canvas.width = 0;
          canvas.height = 0;
          done();
        }, calm ? CALM_MS : LEAVE_MS),
      );
    },
  };
}

function glint(svg: SVGSVGElement) {
  const layer = svg.querySelector(".bshine");
  const sweep = svg.querySelector<SVGAnimationElement>(".bsweep");
  if (!layer || !sweep) return;
  for (const el of svg.querySelectorAll(".bl")) {
    const copy = el.cloneNode(true) as Element;
    copy.removeAttribute("class");
    copy.removeAttribute("style");
    layer.append(copy);
  }
  sweep.beginElement();
}
