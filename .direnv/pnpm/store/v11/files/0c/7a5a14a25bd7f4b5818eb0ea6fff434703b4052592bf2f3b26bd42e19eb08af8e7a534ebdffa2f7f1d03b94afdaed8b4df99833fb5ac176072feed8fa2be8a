declare namespace gsap {

  interface TweenVars {
    morphSVG?: SVGPathValue | gsap.plugins.MorphSVGVars;
  }
}

declare namespace gsap.plugins {

  interface MorphSVGPlugin extends Plugin {
    
    defaultRender?: Function;
    defaultType?: String;
    defaultUpdateTarget?: Boolean;

    /**
     * Converts SVG shapes into <path>s.
     *
     * ```js
     * MorphSVGPlugin.convertToPath("circle");
     * ```
     *
     * @param {DOMTarget} shape
     * @param {boolean} [swap] 
     * @returns {SVGPathElement[]} The converted paths
     * @memberof MorphSVGPlugin
     * @link https://greensock.com/docs/v3/Plugins/MorphSVGPlugin/static.convertToPath()
     */
    convertToPath(shape: string | SVGPrimitive | (string | SVGPrimitive)[], swap?: boolean): SVGPathElement[];

    /**
     * Returns a RawPath associated with whatever is passed in (path data string, selector text, <path> element, or a RawPath)
     *
     * ```js
     * MorphSVGPlugin.getRawPath("#my-path");
     * ```
     *
     * @param {SVGPathValue} path
     * @returns {array} The RawPath
     * @memberof MorphSVGPlugin
     * @link https://greensock.com/docs/v3/Plugins/MorphSVGPlugin/static.getRawPath()
     */
    getRawPath(path: SVGPathValue): string[];

    /**
     * Accepts two strings representing SVG path data and matches the number of points between them, returning an Array with the edited path data strings [shape1, shape2].
     *
     * ```js
     * MorphSVGPlugin.normalizeStrings(shape1, shape2, {map: "complexity"});
     * ```
     *
     * @param {string} shape1
     * @param {string} shape2
     * @param {NormalizeVars} config
     * @returns {string[]} An Array containing the converted string data with matching numbers of points, like [shape1, shape2]
     * @memberof MorphSVGPlugin
     * @link https://greensock.com/docs/v3/Plugins/MorphSVGPlugin/static.normalizeStrings()
     */
    normalizeStrings(shape1: string, shape2: string, config?: NormalizeVars): string[];

    /**
     * Converts a RawPath into a string of path data.
     *
     * ```js
     * MorphSVGPlugin.rawPathToString(myRawPath);
     * ```
     *
     * @param {RawPath} rawPath
     * @returns {string} The converted path data
     * @memberof MorphSVGPlugin
     * @link https://greensock.com/docs/v3/Plugins/MorphSVGPlugin/static.rawPathToString()
     */
    rawPathToString(rawPath: RawPath): string;

    /**
     * Converts a string of path data into a RawPath.
     *
     * ```js
     * MorphSVGPlugin.stringToRawPath("M0,0 C100,20 300,50 400,0...");
     * ```
     *
     * @param {string} data
     * @returns {RawPath} The converted RawPath
     * @memberof MorphSVGPlugin
     * @link https://greensock.com/docs/v3/Plugins/MorphSVGPlugin/static.stringToRawPath()
     */
    stringToRawPath(data: string): RawPath;
  }

  interface NormalizeVars {
    shapeIndex?: number | "auto" | number[];
    map?: "complexity" | "position" | "size";
  }

  interface MorphSVGPluginClass extends MorphSVGPlugin {
    new(): PluginScope & MorphSVGPlugin;
    prototype: PluginScope & MorphSVGPlugin;
  }

  interface MorphSVGVars {
    shape: SVGPathValue;
    type?: "rotational" | "linear";
    origin?: string;
    shapeIndex?: number | "auto" | number[];
    precompile?: "log" | string[];
    map?: "size" | "position" | "complexity";
    render?: Function;
    updateTarget?: boolean;
  }

  const morphSVG: MorphSVGPluginClass;
}

declare const MorphSVGPlugin: gsap.plugins.MorphSVGPlugin;

declare module "gsap/MorphSVGPlugin" {
  export const MorphSVGPlugin: gsap.plugins.MorphSVGPlugin;
  export { MorphSVGPlugin as default };
}

declare module "gsap/dist/MorphSVGPlugin" {
  export * from "gsap/MorphSVGPlugin";
  export { MorphSVGPlugin as default } from "gsap/MorphSVGPlugin";
}

declare module "gsap/src/MorphSVGPlugin" {
  export * from "gsap/MorphSVGPlugin";
  export { MorphSVGPlugin as default } from "gsap/MorphSVGPlugin";
}

declare module "gsap/all" {
  export * from "gsap/MorphSVGPlugin";
}

declare module "gsap-trial/MorphSVGPlugin" {
  export * from "gsap/MorphSVGPlugin";
  export { MorphSVGPlugin as default } from "gsap/MorphSVGPlugin";
}

declare module "gsap-trial/dist/MorphSVGPlugin" {
  export * from "gsap/MorphSVGPlugin";
  export { MorphSVGPlugin as default } from "gsap/MorphSVGPlugin";
}

declare module "gsap-trial/src/MorphSVGPlugin" {
  export * from "gsap/MorphSVGPlugin";
  export { MorphSVGPlugin as default } from "gsap/MorphSVGPlugin";
}

declare module "gsap-trial/all" {
  export * from "gsap/MorphSVGPlugin";
}
