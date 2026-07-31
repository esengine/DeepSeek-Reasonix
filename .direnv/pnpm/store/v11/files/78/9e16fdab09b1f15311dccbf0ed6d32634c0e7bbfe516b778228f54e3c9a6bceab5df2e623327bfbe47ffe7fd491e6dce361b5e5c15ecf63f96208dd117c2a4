declare namespace gsap {

  interface EaseFunction {
    (progress: number): number;
  }
  
  interface Ease {
    easeIn: EaseFunction;
    easeOut: EaseFunction;
    easeInOut: EaseFunction;
  } 

  interface BackConfig extends EaseFunction {
    config(overshoot: number): EaseFunction;
  }

  interface Back {
    easeIn: BackConfig;
    easeOut: BackConfig;
    easeInOut: BackConfig;
  }

  interface EasePack {
    SlowMo: gsap.SlowMo;
    ExpoScaleEase: gsap.ExpoScaleEase;
    RoughEase: gsap.RoughEase;
  }

  interface ElasticConfig extends EaseFunction {
    config(amplitude: number, period?: number): EaseFunction;
  }

  interface Elastic {
    easeIn: ElasticConfig;
    easeOut: ElasticConfig;
    easeInOut: ElasticConfig;
  }

  interface ExpoScaleEase {
    config(startingScale: number, endingScale: number, ease?: string | EaseFunction): EaseFunction;
  }

  interface Linear extends Ease {
    easeNone: EaseFunction;
  }

  interface RoughEaseVars {
    clamp?: boolean;
    points?: number;
    randomize?: boolean;
    strength?: number;
    taper?: "in" | "out" | "both" | "none";
    template?: string | EaseFunction
  }

  interface RoughEaseEase extends EaseFunction {
    config: RoughEaseConfig;
  }

  interface RoughEaseConfig extends EaseFunction {
    (config?: RoughEaseVars): EaseFunction;
  }

  interface RoughEase extends EaseFunction {
    config: RoughEaseConfig;
    ease: RoughEaseEase;   
  }

  interface SlowMoEase extends EaseFunction {
    config: SlowMoConfig;
  }

  interface SlowMoConfig extends EaseFunction {
    (linearRatio: number, power?: number, yoyoMode?: boolean): EaseFunction;
  }

  interface SlowMo extends EaseFunction {
    config: SlowMoConfig;
    ease: SlowMoEase;   
  }

  interface SteppedEase {
    config(steps: number): EaseFunction;
  }
}

declare const Back: gsap.Back;
declare const Bounce: gsap.Ease;
declare const Circ: gsap.Ease;
declare const Cubic: gsap.Ease;
declare const Elastic: gsap.Elastic;
declare const Expo: gsap.Ease;
declare const Linear: gsap.Linear;
declare const Power0: gsap.Linear;
declare const Power1: gsap.Ease;
declare const Power2: gsap.Ease;
declare const Power3: gsap.Ease;
declare const Power4: gsap.Ease;
declare const Quad: gsap.Ease;
declare const Quart: gsap.Ease;
declare const Quint: gsap.Ease;
declare const Sine: gsap.Ease;
declare const SteppedEase: gsap.SteppedEase;
declare const Strong: gsap.Ease;

declare const EasePack: gsap.EasePack;
declare const ExpoScaleEase: gsap.ExpoScaleEase;
declare const RoughEase: gsap.RoughEase;
declare const SlowMo: gsap.SlowMo;

declare module "gsap/gsap-core" {
  export const Back: gsap.Back;
  export const Bounce: gsap.Ease;
  export const Circ: gsap.Ease;
  export const Cubic: gsap.Ease;
  export const Elastic: gsap.Elastic;
  export const Expo: gsap.Ease;
  export const Linear: gsap.Linear;
  export const Power0: gsap.Linear;
  export const Power1: gsap.Ease;
  export const Power2: gsap.Ease;
  export const Power3: gsap.Ease;
  export const Power4: gsap.Ease;
  export const Quad: gsap.Ease;
  export const Quart: gsap.Ease;
  export const Quint: gsap.Ease;
  export const Sine: gsap.Ease;
  export const SteppedEase: gsap.SteppedEase;
  export const Strong: gsap.Ease;
}

declare module "gsap/EasePack" {
  export const EasePack: gsap.EasePack;
  export const ExpoScaleEase: gsap.ExpoScaleEase;
  export const SlowMo: gsap.SlowMo;
  export const RoughEase: gsap.RoughEase;
  export { EasePack as default };
}

declare module "gsap/src/EasePack" {
  export * from "gsap/EasePack";
  export { EasePack as default } from "gsap/EasePack";
}

declare module "gsap/dist/EasePack" {
  export * from "gsap/EasePack";
  export { EasePack as default } from "gsap/EasePack";
}

declare module "gsap/all" {
  export * from "gsap/EasePack";
}

declare module "gsap-trial/EasePack" {
  export * from "gsap/EasePack";
  export { EasePack as default } from "gsap/EasePack";
}

declare module "gsap-trial/src/EasePack" {
  export * from "gsap/EasePack";
  export { EasePack as default } from "gsap/EasePack";
}

declare module "gsap-trial/dist/EasePack" {
  export * from "gsap/EasePack";
  export { EasePack as default } from "gsap/EasePack";
}

declare module "gsap-trial/all" {
  export * from "gsap/EasePack";
}
