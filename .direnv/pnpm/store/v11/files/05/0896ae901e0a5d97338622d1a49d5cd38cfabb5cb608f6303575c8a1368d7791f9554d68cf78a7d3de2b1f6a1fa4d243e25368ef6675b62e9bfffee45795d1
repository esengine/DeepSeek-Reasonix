declare class ScrollToPlugin {

  /**
   * Configure ScrollToPlugin
   *
   * ```js
   * ScrollToPlugin.config({
   *   autoKill: true
   * });
   * ```
   *
   * @static
   * @param {ScrollToPlugin.ConfigVars} vars
   * @memberof ScrollToPlugin
   * @link https://greensock.com/docs/v3/Plugins/ScrollToPlugin/static.config()
   */
  static config(vars: ScrollToPlugin.ConfigVars): void;


  /**
   * Returns the maximum scroll value for the given Element
   *
   * ```js
   * ScrollToPlugin.max(window);
   * ```
   *
   * @static
   * @param {gsap.DOMTarget} element
   * @returns {number} maximum scroll value in pixels
   * @memberof ScrollToPlugin
   * @link https://greensock.com/docs/v3/Plugins/ScrollToPlugin/static.max()
   */
  static max(element: gsap.DOMTarget): number;

  /**
   * Returns the maximum scroll value for the given Element
   *
   * ```js
   * ScrollToPlugin.offset("#target", window);
   * ```
   *
   * @static
   * @param {gsap.DOMTarget} element
   * @param {gsap.DOMTarget} container
   * @returns {number} offset value
   * @memberof ScrollToPlugin
   * @link https://greensock.com/docs/v3/Plugins/ScrollToPlugin/static.offset()
   */
  static offset(element: gsap.DOMTarget, container?: gsap.DOMTarget): number;
}

declare namespace ScrollToPlugin {
  interface Vars {
    x?: number | string | Element;
    y?: number | string | Element;
    offsetX?: number;
    offsetY?: number;
    autoKill?: boolean;
    onAutoKill?: Function;
  }

  interface ConfigVars {
    autoKill?: boolean;
    autoKillThreshold?: number;
  }
}

declare namespace gsap {

  interface TweenVars {
    scrollTo?: number | string | Element | Function | ScrollToPlugin.Vars;
  }
}

declare module "gsap/ScrollToPlugin" {
  class _ScrollToPlugin extends ScrollToPlugin { }
  export {
    _ScrollToPlugin as ScrollToPlugin,
    _ScrollToPlugin as default
  }
}

declare module "gsap/src/ScrollToPlugin" {
  export * from "gsap/ScrollToPlugin";
  export { ScrollToPlugin as default } from "gsap/ScrollToPlugin";
}

declare module "gsap/dist/ScrollToPlugin" {
  export * from "gsap/ScrollToPlugin";
  export { ScrollToPlugin as default } from "gsap/ScrollToPlugin";
}

declare module "gsap/all" {
  export * from "gsap/ScrollToPlugin";
}

declare module "gsap-trial/ScrollToPlugin" {
  export * from "gsap/ScrollToPlugin";
  export { ScrollToPlugin as default } from "gsap/ScrollToPlugin";
}

declare module "gsap-trial/src/ScrollToPlugin" {
  export * from "gsap/ScrollToPlugin";
  export { ScrollToPlugin as default } from "gsap/ScrollToPlugin";
}

declare module "gsap-trial/dist/ScrollToPlugin" {
  export * from "gsap/ScrollToPlugin";
  export { ScrollToPlugin as default } from "gsap/ScrollToPlugin";
}

declare module "gsap-trial/all" {
  export * from "gsap/ScrollToPlugin";
}
