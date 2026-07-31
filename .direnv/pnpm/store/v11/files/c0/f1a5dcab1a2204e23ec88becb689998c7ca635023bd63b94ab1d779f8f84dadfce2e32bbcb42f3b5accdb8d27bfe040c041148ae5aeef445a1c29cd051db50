interface CustomWiggleVars {
  type?: "uniform" | "random" | "anticipate" | "easeOut" | "easeInOut";
  wiggles?: number;
  amplitudeEase?: string | EaseFunction;
  timingEase?: string | EaseFunction;
}

interface EaseFunction {
  (progress: number): number;
}

declare class CustomWiggle {
  ease: EaseFunction;

  constructor(id: string, vars?: CustomWiggleVars);
  
  static create(id: string, vars?: CustomWiggleVars): EaseFunction;
  static register(core: object): void;
}

declare module "gsap/CustomWiggle" {
  class _CustomWiggle extends CustomWiggle {}
  export { _CustomWiggle as CustomWiggle };
  export { _CustomWiggle as default };
}

declare module "gsap/dist/CustomWiggle" {
  export * from "gsap/CustomWiggle";
  export { CustomWiggle as default } from "gsap/CustomWiggle";
}

declare module "gsap/src/CustomWiggle" {
  export * from "gsap/CustomWiggle";
  export { CustomWiggle as default } from "gsap/CustomWiggle";
}

declare module "gsap/all" {
  export * from "gsap/CustomWiggle";
}
