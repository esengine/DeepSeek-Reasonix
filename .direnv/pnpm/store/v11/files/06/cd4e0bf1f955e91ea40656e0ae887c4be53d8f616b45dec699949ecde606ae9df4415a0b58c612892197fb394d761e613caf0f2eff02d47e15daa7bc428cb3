declare namespace gsap {

  type DrawSVGTarget = string | SVGPrimitive | SVGPathElement;

  interface TweenVars {
    drawSVG?: number | BooleanValue | DrawSVGTarget;
  }
}

declare namespace gsap.plugins {

  interface DrawSVGPlugin extends Plugin {

    /**
     * Get the length of an SVG element's stroke.
     *
     * ```js
     * DrawSVGPlugin.getLength(element);
     * ```
     *
     * @param {DrawSVGTarget} element
     * @returns {number} The stroke length
     * @memberof DrawSVGPlugin
     * @link https://greensock.com/docs/v3/Plugins/DrawSVGPlugin/static.getLength()
     */
    getLength(element: DrawSVGTarget): number;

    /**
     * Get the current position of the DrawSVG in array form.
     *
     * ```js
     * DrawSVGPlugin.getPosition(element);
     * ```
     *
     * @param {DrawSVGTarget} element
     * @returns {number[]} The position array
     * @memberof DrawSVGPlugin
     * @link https://greensock.com/docs/v3/Plugins/DrawSVGPlugin/static.getPosition()
     */
    getPosition(element: DrawSVGTarget): number[];
  }

  interface DrawSVGPluginClass extends DrawSVGPlugin {
    new(): PluginScope & DrawSVGPlugin;
    prototype: PluginScope & DrawSVGPlugin;
  }

  const drawSVG: DrawSVGPluginClass;
}

declare const DrawSVGPlugin: gsap.plugins.DrawSVGPlugin;

declare module "gsap/DrawSVGPlugin" {
  export const DrawSVGPlugin: gsap.plugins.DrawSVGPlugin;
  export { DrawSVGPlugin as default };
}

declare module "gsap/src/DrawSVGPlugin" {
  export * from "gsap/DrawSVGPlugin";
  export { DrawSVGPlugin as default } from "gsap/DrawSVGPlugin";
}

declare module "gsap/dist/DrawSVGPlugin" {
  export * from "gsap/DrawSVGPlugin";
  export { DrawSVGPlugin as default } from "gsap/DrawSVGPlugin";
}

declare module "gsap/all" {
  export * from "gsap/DrawSVGPlugin";
}

declare module "gsap-trial/DrawSVGPlugin" {
  export * from "gsap/DrawSVGPlugin";
  export { DrawSVGPlugin as default } from "gsap/DrawSVGPlugin";
}

declare module "gsap-trial/src/DrawSVGPlugin" {
  export * from "gsap/DrawSVGPlugin";
  export { DrawSVGPlugin as default } from "gsap/DrawSVGPlugin";
}

declare module "gsap-trial/dist/DrawSVGPlugin" {
  export * from "gsap/DrawSVGPlugin";
  export { DrawSVGPlugin as default } from "gsap/DrawSVGPlugin";
}

declare module "gsap-trial/all" {
  export * from "gsap/DrawSVGPlugin";
}
