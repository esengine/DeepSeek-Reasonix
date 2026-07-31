declare class ScrollSmoother {

  static readonly version: string;

  readonly scrollTrigger: ScrollTrigger;
  readonly progress: number;
  readonly normalizer?: Observer;

  /**
   * Creates an instance of ScrollSmoother.
   * @param {ScrollSmoother.Vars} vars
   * @memberof ScrollSmoother
   */
  constructor(vars: ScrollSmoother.Vars);

  /**
   * Create a ScrollSmoother instance to smooth the scrolling of the page (only one can exist at any time)
   *
   * ```js
   * ScrollSmoother.create({
   *   content: "#smooth-content",
   *   wrapper: "#smooth-wrapper",
   *   smooth: 1.5,
   *   effects: true
   * });
   * ```
   *
   * @static
   * @param {ScrollSmoother.Vars} vars
   * @returns {ScrollSmoother} The ScrollSmoother
   * @memberof ScrollSmoother
   * @link https://greensock.com/docs/v3/Plugins/ScrollSmoother/static.create()
   */
  static create(vars: ScrollSmoother.Vars): ScrollSmoother;

  /**
   * Returns the ScrollSmoother instance (if one has been created). Only one is allowed at any given time.
   *
   * ```js
   * let smoother = ScrollSmoother.get();
   * ```
   *
   * @static
   * @returns {ScrollSmoother} The ScrollSmoother
   * @memberof ScrollSmoother
   * @link https://greensock.com/docs/v3/Plugins/ScrollSmoother/static.get()
   */
  static get(): ScrollSmoother | undefined;

  /**
   * Refreshes all ScrollTriggers (same as ScrollTrigger.refresh())
   *
   * ```js
   * ScrollSmoother.refresh();
   * ```
   *
   * @param {boolean} safe
   * @memberof ScrollSmoother
   * @link https://greensock.com/docs/v3/Plugins/ScrollSmoother/static.refresh()
   */
  static refresh(safe?: boolean): void;

  /**
   * Sets the content element (the element that moves up and down when scrolling)
   *
   * ```js
   * smoother.content("#content");
   * ```
   *
   * @param {gsap.DOMTarget} element
   * @returns {ScrollSmoother} The ScrollSmoother instance (to make chaining easier)
   * @memberof ScrollSmoother
   * @link https://greensock.com/docs/v3/Plugins/ScrollSmoother/content()
   */
  content(element: gsap.DOMTarget): this;

  /**
   * Gets the content element (the element that moves up and down when scrolling)
   *
   * ```js
   * let el = smoother.content();
   * ```
   *
   * @returns {HTMLElement} The content Element
   * @memberof ScrollSmoother
   * @link https://greensock.com/docs/v3/Plugins/ScrollSmoother/content()
   */
  content(): HTMLElement;

  /**
   * Applies "speed" and/or "lag" effects to the supplied targets (instead of using HTML attributes like data-speed and data-lag)
   *
   * ```js
   * scroller.effects(".box", {
   *    speed: (i, el) => 0.5 + i * 0.1,
   *    lag: 0.5
   * });
   * ```
   *
   * @param {gsap.DOMTarget} targets
   * @param {ScrollSmoother.EffectsVars} vars
   * @returns {ScrollTrigger[]} An Array of ScrollTrigger instances that were created to handle the effects
   * @memberof ScrollSmoother
   * @link https://greensock.com/docs/v3/Plugins/ScrollSmoother/effects()
   */
  effects(targets: gsap.DOMTarget, vars?: ScrollSmoother.EffectsVars | null): ScrollTrigger[];

  /**
   * Gets the ScrollTrigger instances that are managing the effects (like "speed" and/or "lag")
   *
   * ```js
   * let effectTriggers = scroller.effects();
   * ```
   *
   * @returns {ScrollTrigger[]} An Array of ScrollTrigger instances that were created to handle the effects
   * @memberof ScrollSmoother
   * @link https://greensock.com/docs/v3/Plugins/ScrollSmoother/effects()
   */
  effects(): ScrollTrigger[];

  /**
   * Returns the velocity of the vertical scrolling in pixels per second
   *
   * ```js
   * let velocity = smoother.getVelocity()
   * ```
   *
   * @returns {number} The velocity of the vertical scrolling (in pixels per second)
   * @memberof ScrollSmoother
   * @link https://greensock.com/docs/v3/Plugins/ScrollSmoother/getVelocity()
   */
  getVelocity(): number;

  /**
   * Kills the ScrollSmoother instance, reverting the inline CSS of the content and wrapper, removing listeners, etc. This is permanent but you can ScrollSmoother.create() a new one.
   *
   * ```js
   * scrollSmoother.kill();
   * ```
   * 
   * @memberof ScrollSmoother
   * @link https://greensock.com/docs/v3/Plugins/ScrollSmoother/kill()
   */
  kill(): void;

  /**
   * Gets the numeric offset (scroll position) associated with a particular element.
   *
   * ```js
   * let offset = smoother.offset("#id", "center center");
   * ```
   *
   * @param {gsap.DOMTarget} target
   * @param {string} position - like "top center" or "50% bottom-=50px"
   * @param {boolean} ignoreSpeed
   * @returns {number} The numeric offset (scroll position)
   * @memberof ScrollSmoother
   * @link https://greensock.com/docs/v3/Plugins/ScrollSmoother/offset()
   */
  offset(target: gsap.DOMTarget, position?: string, ignoreSpeed?: boolean): number;

  /**
   * Sets the paused state - if true, nothing will scroll (except via .scrollTop() or .scrollTo() on this instance). Serves as a getter and setter function
   *
   * ```js
   * smoother.paused(true);
   * ```
   *
   * @param {boolean} value
   * @returns {ScrollSmoother} The ScrollSmoother instance (for easier chaining)
   * @memberof ScrollSmoother
   * @link https://greensock.com/docs/v3/Plugins/ScrollSmoother/paused()
   */
  paused(value: boolean): this;

  /**
   * Gets the paused state. Serves as a getter and setter function.
   *
   * ```js
   * if (!smoother.paused()) {
   *     ...
   * }
   * ```
   *
   * @returns {boolean} The paused state (true or false)
   * @memberof ScrollSmoother
   * @link https://greensock.com/docs/v3/Plugins/ScrollSmoother/paused()
   */
  paused(): boolean;

  /**
   * Refreshes only the main page's smoothing ScrollTrigger
   *
   * ```js
   * smoother.refresh();
   * ```
   *
   * @param {boolean} soft
   * @param {boolean} force
   * @memberof ScrollSmoother
   * @link https://greensock.com/docs/v3/Plugins/ScrollSmoother/refresh()
   */
  refresh(soft?: boolean, force?: boolean): void;

  /**
   * Scrolls to a particular position or target immediately or in a smooth manner.
   *
   * ```js
   * smoother.scrollTo("#id", true, "center center");
   * ```
   *
   * @param {gsap.DOMTarget | number} target
   * @param {boolean} smooth
   * @param {string} position
   * @returns {void}
   * @memberof ScrollSmoother
   * @link https://greensock.com/docs/v3/Plugins/ScrollSmoother/scrollTo()
   */
  scrollTo(target: gsap.DOMTarget | number, smooth?: boolean, position?: string): void;

  /**
   * Immediately scrolls to a particular numeric scroll position
   *
   * ```js
   * smoother.scrollTop(500);
   * ```
   *
   * @param {number} position
   * @returns {ScrollSmoother} Returns the instance itself for easier chaining
   * @memberof ScrollSmoother
   * @link https://greensock.com/docs/v3/Plugins/ScrollSmoother/scrollTop()
   */
  scrollTop(position: number): this;

  /**
   * Gets the scroll position (numeric offset)
   *
   * ```js
   * let offset = smoother.scrollTop();
   * ```
   *
   * @returns {number} the numeric offset
   * @memberof ScrollSmoother
   * @link https://greensock.com/docs/v3/Plugins/ScrollSmoother/scrollTop()
   */
  scrollTop(): number;
  //
  // /**
  //  * Sets up ScrollTriggers to handle hiding elements (sections) when they're sufficiently outside the viewport in order to improve performance in some situations.
  //  *
  //  * ```js
  //  * smoother.sections("[data-section]");
  //  * ```
  //  *
  //  * @param {gsap.DOMTarget} targets
  //  * @param {ScrollSmoother.SectionVars} vars
  //  * @returns {ScrollTrigger[]} An Array of ScrollTrigger instances that were created to handle the sections
  //  * @memberof ScrollSmoother
  //  * @link https://greensock.com/docs/v3/Plugins/ScrollSmoother/sections()
  //  */
  // sections(targets: gsap.DOMTarget, vars?: ScrollSmoother.SectionVars | null): ScrollTrigger[];
  //
  // /**
  //  * Gets the ScrollTrigger instances that are managing the sections
  //  *
  //  * ```js
  //  * let sectionTriggers = smoother.sections();
  //  * ```
  //  *
  //  * @returns {ScrollTrigger[]} An Array of ScrollTrigger instances that were created to handle the sections
  //  * @memberof ScrollSmoother
  //  * @link https://greensock.com/docs/v3/Plugins/ScrollSmoother/sections()
  //  */
  // sections(): ScrollTrigger[];

  /**
   * Sets the number of seconds it takes to catch up to the scroll position (smoothing).
   *
   * ```js
   * smoother.smooth(1.5);
   * ```
   *
   * @param {number} value
   * @memberof ScrollSmoother
   * @link https://greensock.com/docs/v3/Plugins/ScrollSmoother/smooth()
   */
  smooth(value: number): void;

  /**
   * Gets the number of seconds it takes to catch up to the scroll position (smoothing).
   *
   * ```js
   * let duration = smoother.smooth();
   * ```
   *
   * @returns {number} The amount of smoothing applied (in seconds)
   * @memberof ScrollSmoother
   * @link https://greensock.com/docs/v3/Plugins/ScrollSmoother/smooth()
   */
  smooth(): number;

  /**
   * Sets the wrapper element which serves as the viewport (scrolls the content)
   *
   * ```js
   * smoother.wrapper("#wrapper");
   * ```
   *
   * @param {gsap.DOMTarget} element
   * @returns {ScrollSmoother} The ScrollSmoother instance (to make chaining easier)
   * @memberof ScrollSmoother
   * @link https://greensock.com/docs/v3/Plugins/ScrollSmoother/wrapper()
   */
  wrapper(element: gsap.DOMTarget): this;

  /**
   * Gets the wrapper element which serves as the viewport (scrolls the content)
   *
   * ```js
   * let el = smoother.wrapper();
   * ```
   *
   * @returns {HTMLElement} The wrapper Element
   * @memberof ScrollSmoother
   * @link https://greensock.com/docs/v3/Plugins/ScrollSmoother/wrapper()
   */
  wrapper(): HTMLElement;

}

declare namespace ScrollSmoother {

  type Callback = (self: ScrollSmoother) => any;
  type EventCallback = (self: ScrollSmoother, event: Event) => any;
  type EffectFunc = (index: number, element: Element) => number | string;

  interface EffectsVars {
    speed?: number | string | EffectFunc;
    lag?: number | EffectFunc;
    effectsPadding?: number | string | EffectFunc;
  }
  //
  // interface SectionVars {
  //   add?: boolean;
  // }

  interface Vars {
    autoResize?: boolean;
    content?: gsap.DOMTarget;
    ease?: string | Function;
    effects?: boolean | gsap.DOMTarget;
    effectsPrefix?: string;
    effectsPadding?: number;
    ignoreMobileResize?: boolean;
    normalizeScroll?: boolean | ScrollTrigger.NormalizeVars;
    onFocusIn?: EventCallback;
    onUpdate?: Callback;
    onStop?: Callback;
 //   sections?: boolean | gsap.DOMTarget;
    smooth?: boolean | number;
    smoothTouch?: boolean | number;
    speed?: number;
    wrapper?: gsap.DOMTarget;
  }

}


declare module "gsap/ScrollSmoother" {
  class _ScrollSmoother extends ScrollSmoother { }
  export {
    _ScrollSmoother as ScrollSmoother,
    _ScrollSmoother as default
  }
}

declare module "gsap/dist/ScrollSmoother" {
  export * from "gsap/ScrollSmoother";
  export { ScrollSmoother as default } from "gsap/ScrollSmoother";
}

declare module "gsap/src/ScrollSmoother" {
  export * from "gsap/ScrollSmoother";
  export { ScrollSmoother as default } from "gsap/ScrollSmoother";
}

declare module "gsap/all" {
  export * from "gsap/ScrollSmoother";
}

declare module "gsap-trial/ScrollSmoother" {
  export * from "gsap/ScrollSmoother";
  export { ScrollSmoother as default } from "gsap/ScrollSmoother";
}

declare module "gsap-trial/dist/ScrollSmoother" {
  export * from "gsap/ScrollSmoother";
  export { ScrollSmoother as default } from "gsap/ScrollSmoother";
}

declare module "gsap-trial/src/ScrollSmoother" {
  export * from "gsap/ScrollSmoother";
  export { ScrollSmoother as default } from "gsap/ScrollSmoother";
}

declare module "gsap-trial/all" {
  export * from "gsap/ScrollSmoother";
}
