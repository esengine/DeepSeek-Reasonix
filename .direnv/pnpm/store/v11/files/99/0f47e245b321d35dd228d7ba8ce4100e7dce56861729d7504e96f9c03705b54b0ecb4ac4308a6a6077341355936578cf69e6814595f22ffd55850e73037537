declare namespace PixiPlugin {
  interface PixiMatrix {
    a: number;
    b: number;
    c: number;
    d: number;
    tx: number;
    ty: number;
    array?: number[];
  }

  interface Vars {
    [key: string]: any;
    alpha?: number | string;
    anchor?: number;
    anchorX?: number | string;
    anchorY?: number | string;
    angle?: number | string;
    autoAlpha?: number;
    blur?: number;
    blurX?: number;
    blurY?: number;
    blurPadding?: number;
    brightness?: number;
    colorize?: string | number;
    colorizeAmount?: number;
    colorMatrixFilter?: object; // TODO
    combineCMF?: boolean;
    contrast?: number;
    fillColor?: string | number;
    height?: number | string;
    hue?: number;
    lineColor?: string | number;
    matrix?: PixiMatrix;
    pivot?: number;
    pivotX?: number | string;
    pivotY?: number | string;
    position?: number | string;
    positionX?: number | string;
    positionY?: number | string;
    resolution?: number;
    rotation?: number | string;
    saturation?: number;
    scale?: number | string;
    scaleX?: number | string;
    scaleY?: number | string;
    skew?: number | string;
    skewX?: number | string;
    skewY?: number | string;
    tilePosition?: number;
    tilePositionX?: number | string;
    tilePositionY?: number | string;
    tileScale?: number;
    tileScaleX?: number | string;
    tileScaleY?: number | string;
    tileX?: number | string;
    tileY?: number | string;
    tint?: string | number;
    width?: number | string;
    x?: number | string;
    y?: number | string;
    zIndex?: number | string;
  }
}

declare namespace gsap {

  interface TweenVars {
    pixi?: PixiPlugin.Vars;
  }
}

declare namespace gsap.plugins {

  interface PixiPlugin extends Plugin {

    /**
     * Registers the main PIXI library object with the PixiPlugin so that it can find the 
     * necessary classes/objects. You only need to register it once.
     * 
     * ```js
     * PixiPlugin.registerPIXI(PIXI);
     * ```
     *
     * @param {object} pixi
     * @memberof PixiPlugin
     * @link https://greensock.com/docs/v3/Plugins/PixiPlugin/static.registerPIXI()
     */
    registerPIXI(pixi: object): void;
  }

  interface PixiPluginClass extends PixiPlugin {
    new(): PluginScope & PixiPlugin;
    prototype: PluginScope & PixiPlugin;
  }

  const pixi: PixiPluginClass;
}

declare const PixiPlugin: gsap.plugins.PixiPlugin;

declare module "gsap/PixiPlugin" {
  export const PixiPlugin: gsap.plugins.PixiPlugin;
  export { PixiPlugin as default };
}

declare module "gsap/src/PixiPlugin" {
  export * from "gsap/PixiPlugin";
  export { PixiPlugin as default } from "gsap/PixiPlugin";
}

declare module "gsap/dist/PixiPlugin" {
  export * from "gsap/PixiPlugin";
  export { PixiPlugin as default } from "gsap/PixiPlugin";
}

declare module "gsap/all" {
  export * from "gsap/PixiPlugin";
}

declare module "gsap-trial/PixiPlugin" {
  export * from "gsap/PixiPlugin";
  export { PixiPlugin as default } from "gsap/PixiPlugin";
}

declare module "gsap-trial/src/PixiPlugin" {
  export * from "gsap/PixiPlugin";
  export { PixiPlugin as default } from "gsap/PixiPlugin";
}

declare module "gsap-trial/dist/PixiPlugin" {
  export * from "gsap/PixiPlugin";
  export { PixiPlugin as default } from "gsap/PixiPlugin";
}

declare module "gsap-trial/all" {
  export * from "gsap/PixiPlugin";
}
