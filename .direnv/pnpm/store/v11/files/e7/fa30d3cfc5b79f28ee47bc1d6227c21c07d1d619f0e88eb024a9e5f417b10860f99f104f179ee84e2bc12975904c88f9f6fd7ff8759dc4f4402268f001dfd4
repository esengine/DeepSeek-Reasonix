declare namespace Physics2DPlugin {
  interface Vars {
    acceleration?: gsap.TweenValue;
    accelerationAngle?: gsap.TweenValue;
    angle?: gsap.TweenValue;
    friction?: gsap.TweenValue;
    gravity?: gsap.TweenValue;
    velocity?: gsap.TweenValue;
    xProp?: string;
    yProp?: string;
  }
}

declare namespace gsap {

  interface TweenVars {
    physics2D?: Physics2DPlugin.Vars;
  }
}

declare namespace gsap.plugins {
  interface Physics2DPlugin extends Plugin {
     
  }

  interface Physics2DPluginClass extends Physics2DPlugin {
    new(): PluginScope & Physics2DPlugin;
    prototype: PluginScope & Physics2DPlugin;
  }

  const physics2D: Physics2DPluginClass;
}

declare const Physics2DPlugin: gsap.plugins.Physics2DPlugin;

declare module "gsap/Physics2DPlugin" {
  export const Physics2DPlugin: gsap.plugins.Physics2DPlugin;
  export { Physics2DPlugin as default };
}

declare module "gsap/src/Physics2DPlugin" {
  export * from "gsap/Physics2DPlugin";
  export { Physics2DPlugin as default } from "gsap/Physics2DPlugin";
}

declare module "gsap/dist/Physics2DPlugin" {
  export * from "gsap/Physics2DPlugin";
  export { Physics2DPlugin as default } from "gsap/Physics2DPlugin";
}

declare module "gsap/all" {
  export * from "gsap/Physics2DPlugin";
}

declare module "gsap-trial/Physics2DPlugin" {
  export * from "gsap/Physics2DPlugin";
  export { Physics2DPlugin as default } from "gsap/Physics2DPlugin";
}

declare module "gsap-trial/src/Physics2DPlugin" {
  export * from "gsap/Physics2DPlugin";
  export { Physics2DPlugin as default } from "gsap/Physics2DPlugin";
}

declare module "gsap-trial/dist/Physics2DPlugin" {
  export * from "gsap/Physics2DPlugin";
  export { Physics2DPlugin as default } from "gsap/Physics2DPlugin";
}

declare module "gsap-trial/all" {
  export * from "gsap/Physics2DPlugin";
}
