declare namespace EaselPlugin {
  interface Vars {
    [key: string]: any;
  }
}

declare namespace gsap {

  interface TweenVars {
    easel?: EaselPlugin.Vars;
  }
}

declare namespace gsap.plugins {

  interface EaselPlugin extends Plugin {

  }

  interface EaselPluginClass extends EaselPlugin {
    new(): PluginScope & EaselPlugin;
    prototype: PluginScope & EaselPlugin;
  }

  const easel: EaselPluginClass;
}

declare const EaselPlugin: gsap.plugins.EaselPlugin;

declare module "gsap/EaselPlugin" {
  export const EaselPlugin: gsap.plugins.EaselPlugin;
  export { EaselPlugin as default };
}

declare module "gsap/src/EaselPlugin" {
  export * from "gsap/EaselPlugin";
  export { EaselPlugin as default } from "gsap/EaselPlugin";
}

declare module "gsap/dist/EaselPlugin" {
  export * from "gsap/EaselPlugin";
  export { EaselPlugin as default } from "gsap/EaselPlugin";
}

declare module "gsap/all" {
  export * from "gsap/EaselPlugin";
}

declare module "gsap-trial/EaselPlugin" {
  export * from "gsap/EaselPlugin";
  export { EaselPlugin as default } from "gsap/EaselPlugin";
}

declare module "gsap-trial/src/EaselPlugin" {
  export * from "gsap/EaselPlugin";
  export { EaselPlugin as default } from "gsap/EaselPlugin";
}

declare module "gsap-trial/dist/EaselPlugin" {
  export * from "gsap/EaselPlugin";
  export { EaselPlugin as default } from "gsap/EaselPlugin";
}

declare module "gsap-trial/all" {
  export * from "gsap/EaselPlugin";
}
