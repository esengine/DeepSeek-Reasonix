declare class MotionPathHelper {

  constructor(target: gsap.DOMTarget, vars?: MotionPathHelper.Vars);

  /**
   * Kills the MotionPathHelper instance, removing the editing elements from the DOM.
   *
   * ```js
   * myHelper.kill();
   * ```
   *
   * @memberof MotionPathHelper
   * @link https://greensock.com/docs/v3/Plugins/MotionPathHelper/kill()
   */
  kill(): void

  /**
   * Create a MotionPathHelper instance.
   * 
   * ```js
   * MotionPathHelper.create(".myClass");
   * ```
   *
   * @param {gsap.DOMTarget} target
   * @param {MotionPathHelper.Vars} [vars]
   * @returns {MotionPathHelper} The MotionPathHelper instance
   * @memberof MotionPathHelper
   * @link https://greensock.com/docs/v3/Plugins/MotionPathHelper
   */
  static create(target: gsap.DOMTarget, vars?: MotionPathHelper.Vars): MotionPathHelper;

  /**
   * Makes an SVG <path> editable in the browser.
   *
   * ```js
   * MotionPathHelper.editPath(".myClass", {
   *     onPress: () => console.log("press"),
   *     onRelease: () => console.log("release"),
   *     onUpdate: () => console.log("update")
   * });
   * ```
   *
   * @param {gsap.DOMTarget} target
   * @param {MotionPathHelper.EditPathVars} [vars]
   * @returns {object} A PathEditor instance
   * @memberof MotionPathHelper
   * @link https://greensock.com/docs/v3/Plugins/MotionPathHelper/static.editPath()
   */
  static editPath(target: gsap.DOMTarget, vars?: MotionPathHelper.EditPathVars): MotionPathHelper;
}

declare namespace MotionPathHelper {
  interface Vars {
    [key: string]: any;
    ease?: string | gsap.EaseFunction;
    end?: number;
    duration?: number;
    path?: gsap.DOMTarget;
    pathColor?: gsap.TweenValue;
    pathWidth?: number;
    pathOpacity?: number;
    selected?: boolean;
    start?: number;
  }

  interface EditPathVars {
    [key: string]: any;
    anchorSnap?: Function;
    callbackScope?: object;
    draggable?: boolean;
    handleSize?: number;
    handleSnap?: Function;
    onDeleteAnchor?: Function;
    onPress?: Function;
    onRelease?: Function;
    onUpdate?: Function;
    selected?: boolean;
  }
}

declare module "gsap/MotionPathHelper" {
  class _MotionPathHelper extends MotionPathHelper {}
  export {
    _MotionPathHelper as MotionPathHelper,
    _MotionPathHelper as default
  }
}

declare module "gsap/src/MotionPathHelper" {
  export * from "gsap/MotionPathHelper";
  export { MotionPathHelper as default } from "gsap/MotionPathHelper";
}

declare module "gsap/dist/MotionPathHelper" {
  export * from "gsap/MotionPathHelper";
  export { MotionPathHelper as default } from "gsap/MotionPathHelper";
}

declare module "gsap/all" {
  export * from "gsap/MotionPathHelper";
}

declare module "gsap-trial/MotionPathHelper" {
  export * from "gsap/MotionPathHelper";
  export { MotionPathHelper as default } from "gsap/MotionPathHelper";
}

declare module "gsap-trial/src/MotionPathHelper" {
  export * from "gsap/MotionPathHelper";
  export { MotionPathHelper as default } from "gsap/MotionPathHelper";
}

declare module "gsap-trial/dist/MotionPathHelper" {
  export * from "gsap/MotionPathHelper";
  export { MotionPathHelper as default } from "gsap/MotionPathHelper";
}

declare module "gsap-trial/all" {
  export * from "gsap/MotionPathHelper";
}
