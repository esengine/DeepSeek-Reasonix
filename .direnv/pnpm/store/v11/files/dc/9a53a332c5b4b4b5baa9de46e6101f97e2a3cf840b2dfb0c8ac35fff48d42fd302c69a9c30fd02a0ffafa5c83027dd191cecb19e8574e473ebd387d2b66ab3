declare class SplitText {
  readonly chars: Element[];
  readonly lines: Element[];
  readonly words: Element[];
  readonly masks: Element[];
  readonly elements: Element[];
  readonly isSplit: boolean;

  constructor(target: gsap.DOMTarget, vars?: SplitText.Vars);

    /**
     * Stops any autoSplit behavior (removes internal listeners for resizes and font loading)
     *
     * ```js
     * split.kill();
     * ```
     *
     * @memberof SplitText
     * @link https://greensock.com/docs/v3/Plugins/SplitText/kill()
     */
    kill(): void;

  /**
   * Reverts the innerHTML to the original content.
   * 
   * ```js
   * split.revert();
   * ```
   *
   * @memberof SplitText
   * @link https://greensock.com/docs/v3/Plugins/SplitText/revert()
   */
  revert(): void;

  /**
   * Re-splits a SplitText according to the vars provided. It will automatically call revert() first if necessary. Useful if you want to change the way the text is split after the SplitText instance is created.
   * 
   * ```js
   * split.split({type: "lines,chars"});
   * ```
   *
   * @param {SplitText.Vars} vars
   * @returns {SplitText} The SplitText object created
   * @memberof SplitText
   * @link https://greensock.com/docs/v3/Plugins/SplitText/split()
   */
  split(vars: SplitText.Vars): SplitText;

  /**
   * Creates a SplitText instance according to the vars provided.
   *
   * ```js
   * SplitText.create(".split", {type: "lines,chars"});
   * ```
   *
   * @param {gsap.DOMTarget} target
   * @param {SplitText.Vars} vars
   * @returns {SplitText} The SplitText object created
   * @memberof SplitText
   * @link https://greensock.com/docs/v3/Plugins/SplitText/static.create()
   */
  static create(target: gsap.DOMTarget, vars?: SplitText.Vars): SplitText;

}

declare namespace SplitText {

  type SplitTextTarget = string | NodeList | Node | Node[];
  type PrepareTextFunction = (text: string, element: Element) => string;
  interface WordDelimiterConfig {
    delimiter: RegExp | string;
    replaceWith?: string;
  }
  interface Vars {
    [key: string]: any;
    type: string;
    mask?: "lines" | "words" | "chars";
    wordDelimiter?: string | RegExp | WordDelimiterConfig;
    linesClass?: string;
    wordsClass?: string;
    charsClass?: string;
    aria?: "auto" | "hidden" | "none";
    tag?: string;
    propIndex?: boolean;
    deepSlice?: boolean;
    smartWrap?: boolean;
    specialChars?: string[] | RegExp;
    reduceWhiteSpace?: boolean;
    autoSplit?: boolean;
    ignore?: SplitTextTarget;
    prepareText?: PrepareTextFunction;
    onSplit?: (splitText: SplitText) => void;
    onRevert?: (splitText: SplitText) => void;
    overwrite?: boolean;
  }
}

declare module "gsap/SplitText" {
  class _SplitText extends SplitText {}
  export {
    _SplitText as SplitText,
    _SplitText as default
  }
}

declare module "gsap/src/SplitText" {
  export * from "gsap/SplitText";
  export { SplitText as default } from "gsap/SplitText";
}

declare module "gsap/dist/SplitText" {
  export * from "gsap/SplitText";
  export { SplitText as default } from "gsap/SplitText";
}

declare module "gsap/all" {
  export * from "gsap/SplitText";
}

