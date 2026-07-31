declare namespace gsap {

  type InertiaEndFunction<T> = (value: T) => T;

  interface InertiaDuration {
    min?: number;
    max?: number;
    overshoot?: number; 
  }

  interface InertiaLinkedProps {
    [key: string]: number;
  }

  interface InertiaObject {
    min?: number;
    max?: number;
    end?: number | number[] | InertiaEndFunction<any>;
    // end?: number | number[] | InertiaEndFunction<number> | InertiaEndFunction<InertiaLinkedProps>; 
    velocity?: number | "auto";
  }

  type InertiaVars = {
    duration?: number | InertiaDuration,
    linkedProps?: string,
    resistance?: number
  } & {
    [key: string]: TweenValue | InertiaObject
  };

  interface TweenVars {
    inertia?: InertiaVars;
  }
}

declare namespace gsap.plugins {

  interface InertiaPlugin extends Plugin, VelocityTrackerStatic {
    // TODO add missing methods
    // TODO improve docs on site as well

    /**
     * Returns the current velocity of the given property and target object (only works if you started tracking the property using the InertiaPlugin.track() method).
     *
     * ```js
     * InertiaPlugin.getVelocity(obj, "x,y");
     * ```
     * 
     * @param {Element} target
     * @param {string} props
     * @returns {number} The current velocity
     * @memberof InertiaPlugin
     * @link https://greensock.com/docs/v3/Plugins/InertiaPlugin/static.getVelocity()
     */
     getVelocity(target: Element, props: string): number;
  }

  interface InertiaPluginClass extends InertiaPlugin {
    new(): PluginScope & InertiaPlugin;
    prototype: PluginScope & InertiaPlugin;
  }

  const inertia: InertiaPluginClass;
}

declare const InertiaPlugin: gsap.plugins.InertiaPlugin;

declare module "gsap/InertiaPlugin" {
  export * from "gsap/utils/VelocityTracker";
  export const InertiaPlugin: gsap.plugins.InertiaPlugin;
  export { InertiaPlugin as default };
}

declare module "gsap/dist/InertiaPlugin" {
  export * from "gsap/InertiaPlugin";
  export { InertiaPlugin as default } from "gsap/InertiaPlugin";
}

declare module "gsap/src/InertiaPlugin" {
  export * from "gsap/InertiaPlugin";
  export { InertiaPlugin as default } from "gsap/InertiaPlugin";
}

declare module "gsap/all" {
  export * from "gsap/InertiaPlugin";
}

declare module "gsap-trial/InertiaPlugin" {
  export * from "gsap/InertiaPlugin";
  export { InertiaPlugin as default } from "gsap/InertiaPlugin";
}

declare module "gsap-trial/dist/InertiaPlugin" {
  export * from "gsap/InertiaPlugin";
  export { InertiaPlugin as default } from "gsap/InertiaPlugin";
}

declare module "gsap-trial/src/InertiaPlugin" {
  export * from "gsap/InertiaPlugin";
  export { InertiaPlugin as default } from "gsap/InertiaPlugin";
}

declare module "gsap-trial/all" {
  export * from "gsap/InertiaPlugin";
}
