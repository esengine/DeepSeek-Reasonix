declare class GSDevTools {

  constructor(target: gsap.DOMTarget, vars?: GSDevTools.Vars);

  /**
   * Create a GSDevTools instance.
   * 
   * ```js
   * GSDevTools.create({animation: tl});
   * ```
   *
   * @param {GSDevTools.Vars} vars
   * @returns {GSDevTools} The GSDevTools instance
   * @memberof GSDevTools
   * @link https://greensock.com/docs/v3/Plugins/GSDevTools/static.create()
   */
  static create(vars?: GSDevTools.Vars): GSDevTools;

  /**
   * Returns the GSDevTools instance associated with the provided id.
   *
   * ```js
   * GSDevTools.getById("my-id");
   * ```
   *
   * @param {string} id
   * @returns {GSDevTools} The GSDevTools instance
   * @memberof GSDevTools
   * @link https://greensock.com/docs/v3/Plugins/GSDevTools/static.getById()
   */
  static getById(id: string): GSDevTools | null;

  /**
   * Kills a GSDevTools instance
   *
   * ```js
   * tool.kill();
   * ```
   *
   * @memberof GSDevTools
   * @link https://greensock.com/docs/v3/Plugins/GSDevTools/kill()
   */
  kill(): void
}

declare namespace GSDevTools {
  interface Vars {
    [key: string]: any;
    animation?: string | gsap.core.Animation;
    container?: string | Element;
    css?: object | string;
    globalSync?: boolean;
    hideGlobalTimeline?: boolean;
    id?: string;
    inTime?: number | string;
    keyboard?: boolean;
    loop?: boolean;
    minimal?: boolean;
    outTime?: number | string;
    paused?: boolean;
    persist?: boolean;
    timeScale?: number;
    visibility?: string;
  }
}

declare module "gsap/GSDevTools" {
  class _GSDevTools extends GSDevTools {}
  export {
    _GSDevTools as GSDevTools,
    _GSDevTools as default
  }
}

declare module "gsap/src/GSDevTools" {
  export * from "gsap/GSDevTools";
  export { GSDevTools as default } from "gsap/GSDevTools";
}

declare module "gsap/dist/GSDevTools" {
  export * from "gsap/GSDevTools";
  export { GSDevTools as default } from "gsap/GSDevTools";
}

declare module "gsap/all" {
  export * from "gsap/GSDevTools";
}

declare module "gsap-trial/GSDevTools" {
  export * from "gsap/GSDevTools";
  export { GSDevTools as default } from "gsap/GSDevTools";
}

declare module "gsap-trial/src/GSDevTools" {
  export * from "gsap/GSDevTools";
  export { GSDevTools as default } from "gsap/GSDevTools";
}

declare module "gsap-trial/dist/GSDevTools" {
  export * from "gsap/GSDevTools";
  export { GSDevTools as default } from "gsap/GSDevTools";
}

declare module "gsap-trial/all" {
  export * from "gsap/GSDevTools";
}
