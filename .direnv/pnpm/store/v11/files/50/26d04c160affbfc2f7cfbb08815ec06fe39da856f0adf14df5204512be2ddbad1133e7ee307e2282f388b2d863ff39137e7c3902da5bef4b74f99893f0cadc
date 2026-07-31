declare namespace PhysicsPropsPlugin {
  interface Vars {
    [key: string]: Values;
  }

  interface Values {
    acceleration?: gsap.TweenValue;
    friction?: gsap.TweenValue;
    velocity?: gsap.TweenValue;
  }
}

declare namespace gsap {

  interface TweenVars {
    physicsProps?: PhysicsPropsPlugin.Vars;
  }
}

declare namespace gsap.plugins {
  interface PhysicsPropsPlugin extends Plugin {
     
  }

  interface PhysicsPropsPluginClass extends PhysicsPropsPlugin {
    new(): PluginScope & PhysicsPropsPlugin;
    prototype: PluginScope & PhysicsPropsPlugin;
  }

  const physicsProps: PhysicsPropsPluginClass;
}

declare const PhysicsPropsPlugin: gsap.plugins.PhysicsPropsPlugin;

declare module "gsap/PhysicsPropsPlugin" {
  export const PhysicsPropsPlugin: gsap.plugins.PhysicsPropsPlugin;
  export { PhysicsPropsPlugin as default };
}

declare module "gsap/src/PhysicsPropsPlugin" {
  export * from "gsap/PhysicsPropsPlugin";
  export { PhysicsPropsPlugin as default } from "gsap/PhysicsPropsPlugin";
}

declare module "gsap/dist/PhysicsPropsPlugin" {
  export * from "gsap/PhysicsPropsPlugin";
  export { PhysicsPropsPlugin as default } from "gsap/PhysicsPropsPlugin";
}

declare module "gsap/all" {
  export * from "gsap/PhysicsPropsPlugin";
}

declare module "gsap-trial/PhysicsPropsPlugin" {
  export * from "gsap/PhysicsPropsPlugin";
  export { PhysicsPropsPlugin as default } from "gsap/PhysicsPropsPlugin";
}

declare module "gsap-trial/src/PhysicsPropsPlugin" {
  export * from "gsap/PhysicsPropsPlugin";
  export { PhysicsPropsPlugin as default } from "gsap/PhysicsPropsPlugin";
}

declare module "gsap-trial/dist/PhysicsPropsPlugin" {
  export * from "gsap/PhysicsPropsPlugin";
  export { PhysicsPropsPlugin as default } from "gsap/PhysicsPropsPlugin";
}

declare module "gsap-trial/all" {
  export * from "gsap/PhysicsPropsPlugin";
}
