declare namespace gsap {

  interface AnimationVars {
    scrollTrigger?: gsap.DOMTarget | ScrollTrigger.Vars;
  }
}

declare class ScrollTrigger {

  static readonly version: string;
  static readonly isTouch: number;

  readonly animation?: gsap.core.Animation | undefined;
  readonly callbackAnimation?: gsap.core.Animation | undefined;
  readonly direction: number;
  readonly end: number;
  readonly isActive: boolean;
  readonly pin?: Element;
  readonly progress: number;
  readonly scroller: Element | Window;
  readonly start: number;
  readonly trigger?: Element;
  readonly vars: ScrollTrigger.Vars;

  /**
   * Creates an instance of ScrollTrigger.
   * @param {ScrollTrigger.StaticVars} vars
   * @param {gsap.core.Animation} [animation]
   * @memberof ScrollTrigger
   */
  constructor(vars: ScrollTrigger.StaticVars, animation?:  gsap.core.Animation);

  /**
   * Attach a new event listener to a ScrollTrigger event.
   *
   * ```js
   * ScrollTrigger.addEventListener("scrollStart", myFunc);
   * ```
   *
   * @static
   * @param {"scrollStart" | "scrollEnd" | "refreshInit" | "refresh" | "revert"} event
   * @param {gsap.Callback} callback
   * @memberof ScrollTrigger
   * @link https://greensock.com/docs/v3/Plugins/ScrollTrigger/static.addEventListener()
   */
  static addEventListener(event: "scrollStart" | "scrollEnd" | "refreshInit" | "refresh" | "matchMedia" | "revert", callback: gsap.Callback): void;

  /**
   * Creates a coordinated group of ScrollTriggers (one for each target element) that batch their callbacks within a certain interval
   *
   * ```js
   * ScrollTrigger.batch(".class", {
   *   interval: 0.1,
   *   batchMax: 3,
   *   onEnter: (elements, triggers) => gsap.to(elements, {opacity: 1, stagger: 0.15, overwrite: true}),
   *   onLeave: (elements, triggers) => gsap.set(elements, {opacity: 0, overwrite: true}),
   *   onEnterBack: (elements, triggers) => gsap.to(elements, {opacity: 1, stagger: 0.15, overwrite: true}),
   *   onLeaveBack: (elements, triggers) => gsap.set(elements, {opacity: 0, overwrite: true})
   * });
   * ```
   *
   * @static
   * @param {gsap.DOMTarget} targets
   * @param {ScrollTrigger.BatchVars} vars
   * @returns {ScrollTriggerInstance[]} An Array of the resulting ScrollTrigger instances
   * @memberof ScrollTrigger
   * @link https://greensock.com/docs/v3/Plugins/ScrollTrigger/static.batch()
   */
  static batch(targets: gsap.DOMTarget, vars: ScrollTrigger.BatchVars): ScrollTrigger[];

  /**
   * Un-registers .matchMedia() break points (or just one).
   *
   * ```js
   * ScrollTrigger.clearMatchMedia();
   * ```
   *
   * @static
   * @param {string} [name]
   * @memberof ScrollTrigger
   * @link https://greensock.com/docs/v3/Plugins/ScrollTrigger/static.clearMatchMedia()
   */
  static clearMatchMedia(name?: string): void;

  /**
   * Clears any recorded scroll position data.
   *
   * ```js
   * ScrollTrigger.clearScrollMemory();
   * ```
   *
   * @static
   * @param {string} [scrollRestoration]
   * @memberof ScrollTrigger
   * @link https://greensock.com/docs/v3/Plugins/ScrollTrigger/static.clearScrollMemory()
   */
  static clearScrollMemory(scrollRestoration?: string): void;

  /**
   * Configure ScrollTrigger
   *
   * ```js
   * ScrollTrigger.config({
   *   limitCallbacks: true, 
   *   autoRefreshEvents: "resize,load,visibilitychange,DOMContentLoaded"
   * });
   * ```
   *
   * @static
   * @param {ScrollTrigger.ConfigVars} vars
   * @memberof ScrollTrigger
   * @link https://greensock.com/docs/v3/Plugins/ScrollTrigger/static.config()
   */
  static config(vars: ScrollTrigger.ConfigVars): void;

  /**
   * Create scroll triggers that aren't directly connected to a tween or timeline.
   *
   * ```js
   * ScrollTrigger.create({
   *   trigger: "#id",
   *   start: "top top",
   *   end: "bottom 50%+=100px"
   * });
   * ```
   *
   * @static
   * @param {ScrollTrigger.StaticVars} vars
   * @returns {ScrollTrigger} The ScrollTrigger
   * @memberof ScrollTrigger
   * @link https://greensock.com/docs/v3/Plugins/ScrollTrigger/static.create()
   */
  static create(vars: ScrollTrigger.StaticVars): ScrollTrigger;

  /**
   * Set the default values that apply to every ScrollTrigger upon creation.
   *
   * ```js
   * ScrollTrigger.defaults({
   *   toggleActions: "restart pause resume none",
   *   markers: {startColor: "white", endColor: "white", fontSize: "18px", indent: 10}
   * });
   * ```
   *
   * @static
   * @param {ScrollTrigger.StaticVars} vars
   * @returns {ScrollTrigger.StaticVars} The config object
   * @memberof ScrollTrigger
   * @link https://greensock.com/docs/v3/Plugins/ScrollTrigger/static.defaults()
   */
  static defaults(vars: ScrollTrigger.StaticVars): ScrollTrigger.StaticVars;

  /**
   * Returns all ScrollTriggers that exist.
   *
   * ```js
   * ScrollTrigger.getAll("myID");
   * ```
   *
   * @static
   * @returns {ScrollTrigger[]} The ScrollTrigger
   * @memberof ScrollTrigger
   * @link https://greensock.com/docs/v3/Plugins/ScrollTrigger/static.getAll()
   */
  static getAll(): ScrollTrigger[];

  /**
   * Disables ALL ScrollTrigger functionality.
   *
   * ```js
   * ScrollTrigger.disable();
   * ```
   * @static
   * @param {boolean} reset
   * @param {boolean} kill
   * @memberof ScrollTrigger
   * @link https://greensock.com/docs/v3/Plugins/ScrollTrigger/static.disable()
   */
  static disable(reset?: boolean, kill?: boolean): void;


  /**
   * Enables all ScrollTrigger functionality again after ScrollTrigger.disable() was called.
   *
   * ```js
   * ScrollTrigger.enable();
   * ```
   * @static
   * @memberof ScrollTrigger
   * @link https://greensock.com/docs/v3/Plugins/ScrollTrigger/static.enable()
   */
  static enable(): void;

  /**
   * Returns the ScrollTrigger that was assigned the corresponding id.
   *
   * ```js
   * ScrollTrigger.getById("myID");
   * ```
   *
   * @static
   * @param {string} id
   * @returns {ScrollTriggerInstance} The ScrollTrigger
   * @memberof ScrollTrigger
   * @link https://greensock.com/docs/v3/Plugins/ScrollTrigger/static.getById()
   */
  static getById(id: string): ScrollTrigger | undefined;

  /**
   * Returns a function to control the scroll position of a particular element
   * 
   * ```js
   * let setScroll = ScrollTrigger.getScrollFunc(window);
   * setScroll(250);
   * ```
   *
   * @static
   * @param {(gsap.DOMTarget | Window)} element
   * @param {boolean} [horizontal]
   * @returns {ScrollTrigger.ScrollFunc}
   * @memberof ScrollTrigger
   */
  static getScrollFunc(element: gsap.DOMTarget | Window, horizontal?: boolean): ScrollTrigger.ScrollFunc;

  /**
   * Checks if the element is in the viewport.
   *
   * ```js
   * if (ScrollTrigger.isInViewport(element, 0.2)) {...};
   * ```
   *
   * @static
   * @param {Element | string} element
   * @param {number} [ratio]
   * @param {boolean} [horizontal]
   * @returns {boolean} Boolean
   * @memberof ScrollTrigger
   * @link https://greensock.com/docs/v3/Plugins/ScrollTrigger/static.isInViewport()
   */
  static isInViewport(element: Element | string, ratio?: number, horizontal?: boolean): boolean;

  /**
   * Find out if a ScrollTrigger-related scroller is currently scrolling.
   *
   * ```js
   * ScrollTrigger.isScrolling();
   * ```
   *
   * @static
   * @returns {boolean} Whether or not any scroller is scrolling
   * @memberof ScrollTrigger
   * @link https://greensock.com/docs/v3/Plugins/ScrollTrigger/static.isScrolling()
   */
  static isScrolling(): boolean;

  /**
   * Kills all ScrollTriggers (except the main ScrollSmoother one, if it exists)
   *
   * ```js
   * ScrollTrigger.killAll();
   * ```
   *
   * @static
   * @param {boolean} [allowListeners]
   * @memberof ScrollTrigger
   * @link https://greensock.com/docs/v3/Plugins/ScrollTrigger/static.killAll()
   */
  static killAll(allowListeners?: boolean): void;

  /**
   * Set up ScrollTriggers that only apply to certain viewport sizes using media queries. Deprecated in favor of gsap.matchMedia() in version 3.11.0+
   *
   * ```js
   * ScrollTrigger.matchMedia({
   *   "(min-width: 800px)": function() { },
   *   "(max-width: 799px)": function() { },
   *   "all": function() { }
   * });
   * ```
   * @deprecated
   * @static
   * @param {ScrollTrigger.MatchMediaObject} vars
   * @memberof ScrollTrigger
   * @link https://greensock.com/docs/v3/Plugins/ScrollTrigger/static.matchMedia()
   */
  static matchMedia(vars: ScrollTrigger.MatchMediaObject): void;

  /**
   * Get the maximum scroll value for any given element.
   *
   * ```js
   * ScrollTrigger.maxScroll(window);
   * ```
   *
   * @static
   * @param {(HTMLElement | Window)} target
   * @param {boolean} [horizontal]
   * @returns {number} The max distance the element can scroll
   * @memberof ScrollTrigger
   * @link https://greensock.com/docs/v3/Plugins/ScrollTrigger/static.maxScroll()
   */
  static maxScroll(target: HTMLElement | Window, horizontal?: boolean): number;

  /**
   * Forces scrolling to be done on the JavaScript thread, ensuring it is synchronized and the address bar doesn't show/hide on [most] mobile devices.
   *
   * ```js
   * ScrollTrigger.normalizeScroll(true);
   * ```
   * @static
   * @param {boolean | ScrollTrigger.NormalizeVars | Observer} enable
   * @returns {Observer | undefined} a new Observer instance (if true) or undefined (if false)
   * @memberof ScrollTrigger
   * @link https://greensock.com/docs/v3/Plugins/ScrollTrigger/static.normalizeScroll()
   */
  static normalizeScroll(enable?: boolean | ScrollTrigger.NormalizeVars | Observer): Observer | undefined;

  /**
   * Returns the Observer instance that is currently normalizing scroll behavior (if one exists).
   *
   * ```js
   * let normalizer = ScrollTrigger.normalizeScroll();
   * ```
   * @static
   * @returns {Observer | undefined} the Observer instance normalizing scroll (if one exists) or undefined (if false)
   * @memberof ScrollTrigger
   * @link https://greensock.com/docs/v3/Plugins/ScrollTrigger/static.normalizeScroll()
   */
  static normalizeScroll(): Observer | undefined;

  /**
   * Creates an Observer that senses "scroll-like" behavior like a mouse wheel spin, finger swipe on a touch device, scrollbar drag or even a press/drag of the pointer.
   *
   * ```js
   * ScrollTrigger.observe({
   *     target: ".box",
   *     onUp: self => console.log("up", self.deltaY),
   *     onDown: self => console.log("down", self.deltaY)
   * });
   * ```
   * @static
   * @param {Observer.ObserverVars} vars
   * @returns {Observer} a new Observer instance
   * @memberof ScrollTrigger
   * @link https://greensock.com/docs/v3/Plugins/ScrollTrigger/static.observe()
   */
  static observe(vars:Observer.ObserverVars): Observer;

  /**
   * Returns the position of the Element in the viewport as a normalized value (0-1) where 0 is top/left and 1 is bottom/right.
   *
   * ```js
   * if (ScrollTrigger.positionInViewport(element, "top")) {...};
   * ```
   *
   * @static
   * @param {Element | string} element
   * @param {number} [referencePoint] - a number in pixels from top, percent like "20%" from top or keyword like "top"/"center"/"bottom"
   * @param {boolean} [horizontal]
   * @returns {number} normalized value (0-1) where 0 is top/left and 1 is bottom/right
   * @memberof ScrollTrigger
   * @link https://greensock.com/docs/v3/Plugins/ScrollTrigger/static.positionInViewport()
   */
  static positionInViewport(element: Element | string, referencePoint?: string | number, horizontal?: boolean): number;

  /**
   * Recalculates the positioning of all of the ScrollTriggers on the page.
   *
   * ```js
   * ScrollTrigger.refresh();
   * ```
   *
   * @static
   * @param {boolean} [safe]
   * @memberof ScrollTrigger
   * @link https://greensock.com/docs/v3/Plugins/ScrollTrigger/static.refresh()
   */
  static refresh(safe?: boolean): void;

  /**
   * Registers ScrollTrigger with gsap
   *
   * @static
   * @param {typeof gsap} core
   * @memberof ScrollTrigger
   */
  static register(core: typeof gsap): void;

  /**
   * Removes an event listener for a ScrollTrigger event.
   *
   * ```js
   * ScrollTrigger.removeEventListener("scrollStart", myFunc);
   * ```
   *
   * @static
   * @param {"scrollStart" | "scrollEnd" | "refreshInit" | "refresh" | "matchMedia"} event
   * @param {gsap.Callback} callback
   * @memberof ScrollTrigger
   * @link https://greensock.com/docs/v3/Plugins/ScrollTrigger/static.removeEventListener()
   */
  static removeEventListener(event: "scrollStart" | "scrollEnd" | "refreshInit" | "refresh" | "matchMedia" | "revert", callback: gsap.Callback): void;

  /**
   * Records the current inline CSS styles for the given element(s) so they can be reverted later.
   *
   * ```js
   * ScrollTrigger.saveStyles(".panel, #logo");
   * ```
   *
   * @static
   * @param {gsap.DOMTarget} targets
   * @memberof ScrollTrigger
   * @link https://greensock.com/docs/v3/Plugins/ScrollTrigger/static.targets()
   */
  static saveStyles(targets: gsap.DOMTarget): void;

  /**
   * Sets up proxy methods for a particular scroller so that you can do advanced effects like integrate with a 3rd party smooth scrolling library.
   *
   * ```js
   * ScrollTrigger.scrollerProxy(".container", {
   *   scrollTop(value) {
   *     return arguments.length ? locoScroll.scrollTo(value, 0, 0) : locoScroll.scroll.instance.scroll.y;
   *   },
   *   getBoundingClientRect() {
   *     return {top: 0, left: 0, width: window.innerWidth, height: window.innerHeight};
   *   },
   *   pinType: document.querySelector(".container").style.transform ? "transform" : "fixed"
   * });
   * ```
   *
   * @static
   * @param {gsap.DOMTarget} scroller
   * @param {ScrollTrigger.ScrollerProxyVars} vars
   * @memberof ScrollTrigger
   * @link https://greensock.com/docs/v3/Plugins/ScrollTrigger/static.scrollerProxy()
   */
  static scrollerProxy(scroller: gsap.DOMTarget, vars?: ScrollTrigger.ScrollerProxyVars): void;

  /**
   * Returns a function that will snap in a given direction where 1 is positive and -1 is negative. It will accept an increment or Array of numbers
   *
   * ```js
   * let snap = ScrollTrigger.snapDirectional(5);
   * snap(2, 1); // 5
   * snap(8, -1); // 5
   * snap(51, 1) // 55
   * ```
   *
   * @static
   * @param {number | number[]} incrementOrArray
   * @memberof ScrollTrigger
   * @link https://greensock.com/docs/v3/Plugins/ScrollTrigger/static.snapDirectional()
   */
  static snapDirectional(incrementOrArray: number | number[]): ScrollTrigger.SnapDirectionalFunc;

  /**
   * Sorts the internal Array of ScrollTriggers by "refreshPriority" first, then by their "start" positions (or by a custom function you provide).
   *
   * ```js
   * ScrollTrigger.sort();
   * ```
   *
   * @static
   * @param {Function} [func]
   * @memberof ScrollTrigger
   * @link https://greensock.com/docs/v3/Plugins/ScrollTrigger/static.sort()
   */
  static sort(func?: Function): ScrollTrigger[];

  /**
   * Checks where the scrollbar is and updates all ScrollTrigger instances' progress and direction values accordingly, controls the animation (if necessary) and fires the appropriate callbacks.
   *
   * ```js
   * ScrollTrigger.update();
   * ```
   *
   * @memberof ScrollTrigger
   * @link https://greensock.com/docs/v3/Plugins/ScrollTrigger/static.update()
   */
  static update(): void;

  /**
   * Stops all of the ScrollTrigger's callbacks and removes any added markup and padding caused by pinning.
   *
   * ```js
   * scrollTrigger.disable();
   * scrollTrigger.disable(true);
   * ```
   * 
   * @param {boolean} [revert]
   * @param {boolean} [allowAnimation]
   * @memberof ScrollTrigger
   * @link https://greensock.com/docs/v3/Plugins/ScrollTrigger/disable()
   */
  disable(revert?: boolean, allowAnimation?: boolean): void;

  /**
   * Re-enables a disabled ScrollTrigger instance.
   *
   * ```js
   * scrollTrigger.enable();
   * ```
   * @param {boolean} [reset]
   * @param {boolean} [refresh]
   * @memberof ScrollTrigger
   * @link https://greensock.com/docs/v3/Plugins/ScrollTrigger/enable()
   */
  enable(reset?: boolean, refresh?: boolean): void;

  /**
   * Forces any associated animation (including the callbackAnimation) to its natural end state immediately which is progress(1) if
   * direction is 1 (forward) and progress(0) if direction is -1 (backward).
   *
   * ```js
   * preventOverlaps: self => self.getTrailing().forEach(t => t.endAnimation());
   * ```
   *
   * @memberof ScrollTrigger
   * @link https://greensock.com/docs/v3/Plugins/ScrollTrigger/endAnimation()
   */
  endAnimation(): void;

  /**
   * Gets the scrub tween associated with the ScrollTrigger instance (if scrub was defined), or getTween(true) will get the snap tween (assuming snap was defined).
   *
   * ```js
   * let scrub = scrollTrigger.getTween();
   * scrub.progress(1); // immediately finish the scrub
   * ```
   *
   * @param {boolean} [snap]
   * @memberof ScrollTrigger
   * @link https://greensock.com/docs/v3/Plugins/ScrollTrigger/getTween()
   */
  getTween(snap?: boolean): gsap.core.Tween;

  /**
   * Returns an Array of all ScrollTriggers that precede this one in the updating order according to the current scroll direction.
   *
   * ```js
   * preventOverlaps: self => self.getTrailing().forEach(t => t.endAnimation());
   * ```
   *
   * @param {string | boolean} [name] optional preventOverlaps name to filter
   * @memberof ScrollTrigger
   * @link https://greensock.com/docs/v3/Plugins/ScrollTrigger/getTrailing()
   */
  getTrailing(name?: string | boolean | null): ScrollTrigger[];

  /**
   * Gets the current velocity of the element's scroll on which the ScrollTrigger is attached to (in pixels per second).
   *
   * ```js
   * scrollTrigger.getVelocity();
   * ```
   *
   * @memberof ScrollTrigger
   * @link https://greensock.com/docs/v3/Plugins/ScrollTrigger/getVelocity()
   */
  getVelocity(): number;

  /**
   * Removes all added markup, stops all callbacks, and frees it for GC.
   *
   * ```js
   * scrollTrigger.kill();
   * ```
   *
   * @param {boolean} [reset]
   * @param {boolean} [allowAnimation]
   * @memberof ScrollTrigger
   * @link https://greensock.com/docs/v3/Plugins/ScrollTrigger/kill()
   */
  kill(reset?: boolean, allowAnimation?: boolean): void;

  /**
   * Converts a timeline's label to the associated scroll position (in px)
   *
   * ```js
   * scrollTrigger.labelToScroll("label-1");
   * ```
   *
   * @param {string} label
   * @returns {number} The associated scroll position
   * @memberof ScrollTrigger
   * @link https://greensock.com/docs/v3/Plugins/ScrollTrigger/labelToScroll()
   */
  labelToScroll(label: string): number;

  /**
   * Gets the ScrollTrigger instance that's immediately after this one in the refresh order (if any)
   *
   * ```js
   * scrollTrigger.next();
   * ```
   *
   * @memberof ScrollTrigger
   * @returns {ScrollTrigger | undefined} The next ScrollTrigger (if one exists)
   * @link https://greensock.com/docs/v3/Plugins/ScrollTrigger/next()
   */
  next(): ScrollTrigger | undefined;

  /**
   * Gets the ScrollTrigger instance that's immediately before this one in the refresh order (if any)
   *
   * ```js
   * scrollTrigger.previous();
   * ```
   *
   * @memberof ScrollTrigger
   * @link https://greensock.com/docs/v3/Plugins/ScrollTrigger/previous()
   */
  previous(): ScrollTrigger | undefined;

  /**
   * Forces the ScrollTrigger instance to re-calculate its start and end values (the scroll positions where it'll be activated).
   *
   * ```js
   * scrollTrigger.refresh();
   * ```
   *
   * @memberof ScrollTrigger
   * @link https://greensock.com/docs/v3/Plugins/ScrollTrigger/refresh()
   */
  refresh(): void;

  /**
   * Gets the scroll position of the ScrollTrigger's scroller.
   *
   * ```js
   * scrollTrigger.scroll();
   * ```
   *
   * @returns {number} The scroll position of the scroller
   * @memberof ScrollTrigger
   * @link https://greensock.com/docs/v3/Plugins/ScrollTrigger/scroll()
   */
  scroll(): number;

  /**
   * Sets the scroll position of the ScrollTrigger's scroller.
   *
   * ```js
   * scrollTrigger.scroll(100);
   * ```
   *
   * @param {number} position
   * @memberof ScrollTrigger
   * @link https://greensock.com/docs/v3/Plugins/ScrollTrigger/scroll()
   */
  scroll(position: number): void;

  /**
   * Animates the scroll position of the ScrollTrigger's scroller.
   *
   * ```js
   * scrollTrigger.tweenTo(100);
   * ```
   *
   * @param {number} position
   * @returns {gsap.core.Tween} Tween
   * @memberof ScrollTrigger
   */
  tweenTo(position: number): gsap.core.Tween;

  update(reset?: boolean, recordVelocity?: boolean, forceFake?: boolean): void;
}

declare namespace ScrollTrigger {

  interface RectObj {
    top: number;
    left: number;
    width: number;
    height: number;
  }

  interface MatchMediaObject {
    [key: string]: Function;
  }

  type Callback = (self: ScrollTrigger) => any;
  type BatchCallback = (targets: Element[], triggers: ScrollTrigger[]) => any;
  type NumFunc = () => number;
  type SnapFunc = (value: number, self?: ScrollTrigger) => number;
  type SnapDirectionalFunc = (value: number, direction?: number, threshold?: number) => number;
  type GetterSetterNumFunc = (value?: number) => number | void;
  type GetterRectFunc = () => RectObj;
  type StartEndFunc = (self: ScrollTrigger) => string | number;
  type ScrollFunc = (position: number) => void;

  interface MarkersVars {
    endColor?: string;
    fontSize?: string;
    fontWeight?: string;
    indent?: number;
    startColor?: string;
  }

  interface ToggleClassVars {
    className: string;
    targets?: gsap.DOMTarget;
  }

  interface SnapVars {
    delay?: number;
    duration?: number | RangeObject;
    inertia?: boolean;
    ease?: string | gsap.EaseFunction;
    snapTo?: number | number[] | "labels" | "labelsDirectional" | SnapFunc;
    directional?: boolean;
    onInterrupt?: Callback;
    onStart?: Callback;
    onComplete?: Callback;
  }

  interface RangeObject {
    min?: number;
    max?: number;
  }

  interface Vars {
    anticipatePin?: number;
    containerAnimation?: gsap.core.Animation;
    end?: string | number | StartEndFunc;
    endTrigger?: gsap.DOMTarget;
    fastScrollEnd?: boolean | number;
    horizontal?: boolean;
    id?: string;
    immediateRender?: boolean; 
    invalidateOnRefresh?: boolean;
    markers?: boolean | MarkersVars;
    once?: boolean;
    onEnter?: Callback;
    onEnterBack?: Callback;
    onKill?: Callback;
    onLeave?: Callback;
    onLeaveBack?: Callback;
    onRefresh?: Callback;
    onRefreshInit?: Callback;
    onSnapComplete?: Callback;
    onScrubComplete?: Callback;
    onUpdate?: Callback;
    onToggle?: Callback;
    pin?: boolean | gsap.DOMTarget;
    pinnedContainer?: gsap.DOMTarget;
    pinReparent?: boolean;
    pinSpacing?: boolean | string;
    pinSpacer?: gsap.DOMTarget;
    pinType?: "fixed" | "transform";
    preventOverlaps?: boolean | string | Callback;
    refreshPriority?: number;
    scroller?: gsap.DOMTarget | Window;
    scrub?: boolean | number;
    snap?: number | number[] | "labels" | "labelsDirectional" | SnapFunc | SnapVars;
    start?: string | number | StartEndFunc;
    toggleActions?: string;
    toggleClass?: string | ToggleClassVars;
    trigger?: gsap.DOMTarget;
  }

  interface StaticVars extends Vars {
    animation?: gsap.core.Animation;
  }

  interface BatchVars {
    interval?: number;
    batchMax?: number | NumFunc;
    anticipatePin?: number;
    end?: string | number | StartEndFunc;
    fastScrollEnd?: boolean | number;
    horizontal?: boolean;
    once?: boolean;
    onEnter?: BatchCallback;
    onEnterBack?: BatchCallback;
    onLeave?: BatchCallback;
    onLeaveBack?: BatchCallback;
    onRefresh?: BatchCallback;
    onRefreshInit?: Callback;
    onUpdate?: BatchCallback;
    onToggle?: BatchCallback;
    pin?: boolean | gsap.DOMTarget;
    pinReparent?: boolean;
    pinSpacing?: boolean | string;
    pinSpacer?: gsap.DOMTarget;
    pinType?: "fixed" | "transform";
    preventOverlaps?: boolean | string | Callback;
    scroller?: gsap.DOMTarget | Window;
    start?: string | number | StartEndFunc;
    toggleClass?: string | ToggleClassVars;
  }

  interface ConfigVars {
    limitCallbacks?: boolean;
    syncInterval?: number;
    autoRefreshEvents?: string;
    ignoreMobileResize?: boolean;
  }

  interface ScrollerProxyVars {
    scrollTop?: GetterSetterNumFunc;
    scrollLeft?: GetterSetterNumFunc;
    scrollWidth?: GetterSetterNumFunc;
    scrollHeight?: GetterSetterNumFunc;
    fixedMarkers?: boolean;
    getBoundingClientRect?: GetterRectFunc;
    pinType?: "fixed" | "transform";
    content?: gsap.DOMTarget;
  }
  interface NormalizeVars extends Observer.ObserverVars {
    momentum?: number | Function;
    content?: gsap.DOMTarget;
    allowNestedScroll?: boolean;
  }

}


declare namespace gsap.plugins {

  /**
   * @deprecated since 3.7.0
   * @see ScrollTrigger.ScrollerProxyVars
   */
  type ScrollerProxyVars = ScrollTrigger.ScrollerProxyVars;

  /**
   * @deprecated since 3.7.0
   * @see ScrollTrigger
   */
  type ScrollTrigger = any;

  /**
   * @deprecated since 3.7.0
   * @see ScrollTrigger.BatchVars
   */
  type ScrollTriggerBatchVars = ScrollTrigger.BatchVars;

  /**
   * @deprecated since 3.7.0
   * @see ScrollTrigger.ConfigVars
   */
  type ScrollTriggerConfigVars = ScrollTrigger.ConfigVars;

  /**
   * @deprecated since 3.7.0
   * @see ScrollTrigger
   */
  class ScrollTriggerInstance extends ScrollTrigger {}

  /**
   * @deprecated since 3.7.0
   * @see ScrollTrigger.Vars
   */
  type ScrollTriggerInstanceVars = ScrollTrigger.Vars;

  /**
   * @deprecated since 3.7.0
   * @see ScrollTrigger
   */
  class ScrollTriggerStatic extends ScrollTrigger {}

  /**
   * @deprecated since 3.7.0
   * @see ScrollTrigger.StaticVars
   */
  type ScrollTriggerStaticVars = ScrollTrigger.StaticVars;

  /**
   * @deprecated since 3.7.0
   * @see ScrollTrigger.SnapVars;
   */
  type SnapVars = ScrollTrigger.SnapVars;

  /**
   * @deprecated since 3.7.0
   * @see ScrollTrigger.ToggleClassVars
   */
  type ToggleClassVars = ScrollTrigger.ToggleClassVars;
}

declare module "gsap/ScrollTrigger" {
  class _ScrollTrigger extends ScrollTrigger { }
  export {
    _ScrollTrigger as ScrollTrigger,
    _ScrollTrigger as default
  }
}

declare module "gsap/dist/ScrollTrigger" {
  export * from "gsap/ScrollTrigger";
  export { ScrollTrigger as default } from "gsap/ScrollTrigger";
}

declare module "gsap/src/ScrollTrigger" {
  export * from "gsap/ScrollTrigger";
  export { ScrollTrigger as default } from "gsap/ScrollTrigger";
}

declare module "gsap/all" {
  export * from "gsap/ScrollTrigger";
}

declare module "gsap-trial/ScrollTrigger" {
  export * from "gsap/ScrollTrigger";
  export { ScrollTrigger as default } from "gsap/ScrollTrigger";
}

declare module "gsap-trial/dist/ScrollTrigger" {
  export * from "gsap/ScrollTrigger";
  export { ScrollTrigger as default } from "gsap/ScrollTrigger";
}

declare module "gsap-trial/src/ScrollTrigger" {
  export * from "gsap/ScrollTrigger";
  export { ScrollTrigger as default } from "gsap/ScrollTrigger";
}

declare module "gsap-trial/all" {
  export * from "gsap/ScrollTrigger";
}
