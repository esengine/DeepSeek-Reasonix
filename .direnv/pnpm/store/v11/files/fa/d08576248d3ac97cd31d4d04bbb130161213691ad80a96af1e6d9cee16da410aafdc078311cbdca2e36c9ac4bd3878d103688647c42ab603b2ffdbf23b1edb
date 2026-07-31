declare namespace ScrambleTextPlugin {
  interface Vars {
    text: string;
    chars?: string;
    speed?: number;
    delimiter?: string;
    tweenLength?: boolean;
    newClass?: string;
    oldClass?: string;
    revealDelay?: number;
    rightToLeft?: boolean;
  }
}

declare namespace gsap {

  interface TweenVars {
    scrambleText?: string | ScrambleTextPlugin.Vars;
  }
}

declare namespace gsap.plugins {
  interface ScrambleTextPlugin extends Plugin {
     
  }

  interface ScrambleTextPluginClass extends ScrambleTextPlugin {
    new(): PluginScope & ScrambleTextPlugin;
    prototype: PluginScope & ScrambleTextPlugin;
  }

  const scrambleText: ScrambleTextPluginClass;
}

declare const ScrambleTextPlugin: gsap.plugins.ScrambleTextPlugin;

declare module "gsap/ScrambleTextPlugin" {
  export const ScrambleTextPlugin: gsap.plugins.ScrambleTextPlugin;
  export { ScrambleTextPlugin as default };
}

declare module "gsap/src/ScrambleTextPlugin" {
  export * from "gsap/ScrambleTextPlugin";
  export { ScrambleTextPlugin as default } from "gsap/ScrambleTextPlugin";
}

declare module "gsap/dist/ScrambleTextPlugin" {
  export * from "gsap/ScrambleTextPlugin";
  export { ScrambleTextPlugin as default } from "gsap/ScrambleTextPlugin";
}

declare module "gsap/all" {
  export * from "gsap/ScrambleTextPlugin";
}

declare module "gsap-trial/ScrambleTextPlugin" {
  export * from "gsap/ScrambleTextPlugin";
  export { ScrambleTextPlugin as default } from "gsap/ScrambleTextPlugin";
}

declare module "gsap-trial/src/ScrambleTextPlugin" {
  export * from "gsap/ScrambleTextPlugin";
  export { ScrambleTextPlugin as default } from "gsap/ScrambleTextPlugin";
}

declare module "gsap-trial/dist/ScrambleTextPlugin" {
  export * from "gsap/ScrambleTextPlugin";
  export { ScrambleTextPlugin as default } from "gsap/ScrambleTextPlugin";
}

declare module "gsap-trial/all" {
  export * from "gsap/ScrambleTextPlugin";
}
