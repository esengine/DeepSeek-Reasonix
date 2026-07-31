declare namespace gsap {

  type RegisterablePlugins = 
    | Ease
    | EasePack
    | ExpoScaleEase
    | Plugin
    | RoughEase
    | SteppedEase
    | VelocityTracker
    | typeof core.Animation
    | typeof core.Tween
    | typeof core.Timeline
    | typeof Draggable
    | typeof GSDevTools
    | typeof MotionPathHelper
    | typeof SplitText
    | typeof Flip
    | typeof ScrollTrigger
    | typeof Observer
    | typeof ScrollSmoother;

  // querySelector returns type Element | null
  type DOMTarget = Element | string | null | Window | ArrayLike<Element | string | Window | null>;
  type TweenTarget = string | object | null; 

  type Callback = (...args: any[]) => any;
  type ContextSafeFunc = (func: Function) => Function;
  type ContextFunc = (context: Context, contextSafe?: ContextSafeFunc) => Function | any | void;
  type CallbackType = "onComplete" | "onInterrupt" | "onRepeat" | "onReverseComplete" | "onStart" | "onUpdate";
  type TickerCallback = (time: number, deltaTime: number, frame: number, elapsed: number) => void | null;

  type Point2D = { x: number, y: number };
  type Position = number | string;
  
  type FunctionBasedValue<T> = (index: number, target: any, targets: any[]) => T;
  type ArrayValue = any[] | FunctionBasedValue<any[]>;
  type BooleanValue = boolean | FunctionBasedValue<boolean>;
  type NumberValue = number | FunctionBasedValue<number>;
  type StringValue = string | FunctionBasedValue<string>;
  type ElementValue = Element | FunctionBasedValue<Element>;
  type TweenValue = NumberValue | StringValue;
  type QuickToFunc = {
    (value: number, start?: number, startIsRelative?: boolean): core.Tween;
    tween: core.Tween;
  }
  
  type SVGPathValue = string | SVGPathElement;
  type SVGPathTarget = SVGPathValue | ArrayLike<SVGPathValue>;
  type SVGPrimitive = SVGCircleElement | SVGRectElement | SVGEllipseElement | SVGPolygonElement | SVGPolylineElement | SVGLineElement;

  interface Conditions {
    [key: string]: boolean;
  }
  interface Context {
    [key: string]: Function | any;
    selector?: Function;
    isReverted: boolean;
    conditions?: Conditions;
    queries?: object;
    add(methodName: string, func: Function, scope?: Element | string | object): Function;
    add<T extends (...args: any[]) => any>(func: T, scope?: Element | string | object): ReturnType<T>;
    ignore(func: Function): void;
    kill(revert?: boolean): void;
    revert(config?: object): void;
    clear(): void;
  }

  interface MatchMedia {
    contexts: Context[];
    add(conditions: string | object, func: ContextFunc, scope?: Element | string | object): MatchMedia;
    revert(config?: object): void;
    kill(revert?: boolean):void;
  }

  export interface AnimationVars extends CallbackVars {
    [key: string]: any;
    data?: any;    
    id?: string | number;
    inherit?: boolean;
    paused?: boolean;
    repeat?: number;
    repeatDelay?: number;
    repeatRefresh?: boolean;
    reversed?: boolean;
    yoyo?: boolean;
  }  

  interface CallbackVars {
    callbackScope?: object;
    onComplete?: Callback;
    onCompleteParams?: any[];
    onRepeat?: Callback;
    onRepeatParams?: any[];
    onReverseComplete?: Callback;
    onReverseCompleteParams?: any[];
    onStart?: Callback;
    onStartParams?: any[];
    onUpdate?: Callback;
    onUpdateParams?: any[];
  }

  interface EaseMap {
    [key: string]: EaseFunction;
  }

  interface EffectsMap {
    [key: string]: any;
  }

  interface GSAPConfig {
    autoKillThreshold?: number;
    autoSleep?: number;
    force3D?: "auto" | boolean;
    nullTargetWarn?: boolean;
    resistance?: number;
    stringFilter?: Callback; // TODO: Find out signature
    unitFactors?: { time?: number, totalTime?: number };
    units?: GSAPUnits
  }

  type GSAPUnits = {
    bottom?: string
    fontSize?: string
    height?: string
    left?: string
    lineHeight?: string
    margin?: string
    padding?: string
    perspective?: string
    right?: string
    rotation?: string
    rotationX?: string
    rotationY?: string
    skewX?: string
    skewY?: string
    top?: string
    width?: string
    x?: string
    y?: string
    z?: string
  } & {
    [key: string]: string
  }
  
  interface StaggerVars extends CallbackVars, utils.DistributeConfig {
    repeat?: number;
    repeatDelay?: number;
    repeatRefresh?: boolean;
    yoyo?: boolean;
    /** @deprecated since version 3.15.0. Use `easeReverse` instead. **/
    yoyoEase?: boolean | string | EaseFunction;
    easeReverse?: boolean | EaseString | EaseFunction;
  }

  interface Ticker {
    add(callback: TickerCallback, once?: boolean, prioritize?: boolean): Callback;
    fps(fps: number): void;
    frame: number;
    lagSmoothing(threshold: number | boolean, adjustedLag?: number): void;
    remove(callback: Callback): void;
    sleep(): void;
    tick(): void;
    time: number;
    deltaRatio(fps?: number): number;
    wake(): void;
  }

  interface TimelineVars extends AnimationVars {
    autoRemoveChildren?: boolean;
    defaults?: TweenVars;
    delay?: number;
    smoothChildTiming?: boolean;
  }

  type EaseString = "none"
      | "power1" | "power1.in" | "power1.out" | "power1.inOut"
      | "power2" | "power2.in" | "power2.out" | "power2.inOut"
      | "power3" | "power3.in" | "power3.out" | "power3.inOut"
      | "power4" | "power4.in" | "power4.out" | "power4.inOut"
      | "back" | "back.in" | "back.out" | "back.inOut"
      | "bounce" | "bounce.in" | "bounce.out" | "bounce.inOut"
      | "circ" | "circ.in" | "circ.out" | "circ.inOut"
      | "elastic" | "elastic.in" | "elastic.out" | "elastic.inOut"
      | "expo" | "expo.in" | "expo.out" | "expo.inOut"
      | "sine" | "sine.in" | "sine.out" | "sine.inOut" | ({} & string);

  export interface TweenVars extends AnimationVars {
    delay?: TweenValue;
    duration?: TweenValue;
    ease?: EaseString | EaseFunction;
    endArray?: any[];
    immediateRender?: boolean;    
    lazy?: boolean;
    keyframes?: TweenVars[] | object;
    onInterrupt?: Callback;
    onInterruptParams?: any[];
    overwrite?: "auto" | boolean;
    runBackwards?: boolean;
    stagger?: NumberValue | StaggerVars;
    startAt?: TweenVars;
    /** @deprecated since version 3.15.0. Use `easeReverse` instead. **/
    yoyoEase?: boolean | string | EaseFunction;
    easeReverse?: boolean | EaseString | EaseFunction;
  }

  const effects: EffectsMap;

  const globalTimeline: core.Timeline;

  const ticker: Ticker;

  const version: string;

  /**
   * Gets or sets GSAP's global configuration settings.
   * 
   * Options: autoSleep, force3D, nullTargetWarn, and units
   * 
   * ```js
   * gsap.config({force3D: false});
   * ```
   *
   * @param {GSAPConfig} [config]
   * @returns {GSAPConfig} Configuration object
   * @memberof gsap
   * @link https://greensock.com/docs/v3/GSAP/gsap.config()
   */
  function config(config?: GSAPConfig): GSAPConfig;

  /**
   * Creates a Context object for recording/reverting any GSAP animations and/or ScrollTriggers that are in the provided function
   *
   * ```js
   * let ctx = gsap.context((self) => {
   *     gsap.to(".box", {x: 100});
   * }, myElement);
   *
   * // then later
   * ctx.revert();
   * ```
   *
   * @param {ContextFunc} [func]
   * @param {Element | string | object} [scope]
   * @returns {Context} Context object
   * @memberof gsap
   * @link https://greensock.com/docs/v3/GSAP/gsap.context()
   */
  function context(func?: ContextFunc, scope?: Element | string | object): Context;

  /**
   * Gets or sets GSAP's global defaults. These will be inherited by every tween.
   * 
   * ```js
   * gsap.defaults({ease: "none", duration: 1});
   * ```
   *
   * @param {TweenVars} [defaults]
   * @returns {TweenVars} Defaults object
   * @memberof gsap
   * @link https://greensock.com/docs/v3/GSAP/gsap.defaults()
   */
  function defaults(defaults?: TweenVars): TweenVars;

  /**
   * Delays the call of a function by the specified amount.
   *
   * ```js
   * let delayTween = gsap.delayedCall(1, myFunc);
   * ```
   *
   * @param {number} delay
   * @param {Function} callback
   * @param {any[]} [params]
   * @returns {Tween} Tween instance
   * @memberof gsap
   * @link https://greensock.com/docs/v3/GSAP/gsap.delayedCall()
   */
  function delayedCall(delay: number, callback: Function, params?: any[]): core.Tween;

  /**
   * Transfers all tweens, timelines, and (optionally) delayed calls from the root timeline into a new timeline.
   *
   * ```js
   * let exportedTL = gsap.exportRoot();
   * ```
   *
   * @param {TimelineVars} [vars]
   * @param {boolean} [includeDelayedCalls]
   * @returns {Timeline} Timeline instance
   * @memberof gsap
   * @link https://greensock.com/docs/v3/GSAP/gsap.exportRoot()
   */
  function exportRoot(vars?: TimelineVars, includeDelayedCalls?: boolean): core.Timeline;

  /**
   * Creates a tween coming FROM the given values.
   *
   * ```js
   * gsap.from(".class", {x: 100});
   * ```
   *
   * @param {TweenTarget} targets
   * @param {TweenVars} vars
   * @returns {Tween} Tween instance
   * @memberof gsap
   * @link https://greensock.com/docs/v3/GSAP/gsap.from()
   */
  function from(targets: TweenTarget, vars: TweenVars): core.Tween;

   /**
   * Creates a tween coming FROM the first set of values going TO the second set of values.
   *
   * ```js
   * gsap.fromTo(".class", {x: 0}, {x: 100});
   * ```
   *
   * @param {TweenTarget} targets
   * @param {TweenVars} fromVars
   * @param {TweenVars} toVars
   * @returns {Tween} Tween instance
   * @memberof gsap
   * @link https://greensock.com/docs/v3/GSAP/gsap.fromTo()
   */
  function fromTo(targets: TweenTarget, fromVars: TweenVars, toVars: TweenVars): core.Tween;
  /**
   * **Deprecated method signature.** Use the `duration` property instead.
   * 
   * ```js
   * gsap.fromTo(".class", 1, {x: 0}, {x: 100});
   * ```
   * @deprecated since version 3.0.0
   * @param {TweenTarget} targets
   * @param {number} duration - The duration parameter is deprecated. Use the `duration` property instead.
   * @param {TweenVars} fromVars
   * @param {TweenVars} toVars
   * @returns {Tween} Tween instance
   * @link https://greensock.com/docs/v3/GSAP/gsap.fromTo()
   */
  function fromTo(targets: TweenTarget, duration: number, fromVars: TweenVars, toVars: TweenVars): core.Tween;

  /**
   * Gets the tween or timeline with the specified ID if it exists.
   *
   * ```js
   * gsap.to(obj, {id: "myTween", x: 100});
   * 
   * // later
   * let tween = gsap.getById("myTween");
   * ```
   *
   * @param {string | number} id
   * @returns {Tween | undefined} Tween instance
   * @memberof gsap
   * @link https://greensock.com/docs/v3/GSAP/gsap.getById()
   */
  function getById<T extends core.Animation>(id: string | number): T | undefined;

  /**
   * Gets the specified property of the target (or first of the targets) if it exists.
   *
   * ```js
   * gsap.getProperty(element, "x");
   * ```
   *
   * @param {TweenTarget} target
   * @param {string} property
   * @param {string} [unit]
   * @returns {string | number} Value
   * @memberof gsap
   * @link https://greensock.com/docs/v3/GSAP/gsap.getProperty()
   */
  function getProperty(target: TweenTarget, property: string, unit?: string): string | number;
  function getProperty(target: TweenTarget): (property: string, unit?: string) => string | number;

  /**
   * Gets all of the tweens whose targets include the specified target or group of targets.
   *
   * ```js
   * gsap.getTweensOf(element);
   * ```
   *
   * @param {TweenTarget} targets
   * @param {boolean} [onlyActive]
   * @returns {Tween} Tween instance
   * @memberof gsap
   * @link https://greensock.com/docs/v3/GSAP/gsap.getTweensOf()
   */
  function getTweensOf(targets: TweenTarget, onlyActive?: boolean): core.Tween[];

  /**
   * Used to add all the GSAP globals to a particular tween object.
   *
   * ```js
   * gsap.install(myTween);
   * ```
   * 
   * @param {object} targets
   * @returns {gsap} The gsap object
   * @memberof gsap
   */
  function install(targets: object): typeof gsap;

  /**
   * Reports whether or not a particular object is actively animating.
   *
   * ```js
   * gsap.isTweening("#id");
   * ```
   * 
   * @param {TweenTarget} targets
   * @returns {boolean} Status
   * @memberof gsap
   * @link https://greensock.com/docs/v3/GSAP/gsap.isTweening()
   */
  function isTweening(targets: TweenTarget): boolean;

  /**
   * Kills all the tweens (or specific tweening properties) of a particular object or the delayedCalls to a particular function.
   *
   * ```js
   * gsap.killTweensOf(".myClass");
   * gsap.killTweensOf(myObject, "opacity,x");
   * ```
   *
   * @param {TweenTarget} targets
   * @param {object | string} [properties]
   * @param {boolean} [onlyActive]
   * @returns {void} Void
   * @memberof gsap
   * @link https://greensock.com/docs/v3/GSAP/gsap.killTweensOf()
   */
  function killTweensOf(targets: TweenTarget, properties?: object | string, onlyActive?: boolean): void;

  /**
   * Creates a MatchMedia object for adding functions that run when a media query matches
   *
   * ```js
   * let mm = gsap.matchMedia(myElement);
   * mm.add("(max-width: 500px)", (context) => {
   *     gsap.to(".box", {x: 100});
   * });
   * ```
   *
   * @param {Element | string | object} [scope]
   * @returns {MatchMedia} MatchMedia object
   * @memberof gsap
   * @link https://greensock.com/docs/v3/GSAP/gsap.matchMedia()
   */
  function matchMedia(scope?: Element | string | object): MatchMedia;

  /**
   * Immediately reverts all active/matching MatchMedia objects and then runs any that currently match.
   *
   * ```js
   * gsap.matchMediaRefresh();
   * ```
   *
   * @memberof gsap
   * @link https://greensock.com/docs/v3/GSAP/gsap.matchMediaRefresh()
   */
  function matchMediaRefresh(): void;

  /**
   * Returns the corresponding easing function for the given easing string.
   *
   * ```js
   * let ease = gsap.parseEase("power1");
   * ```
   *
   * @param {EaseString | EaseFunction} ease
   * @returns {EaseFunction} Ease function
   * @memberof gsap
   * @link https://greensock.com/docs/v3/GSAP/gsap.parseEase()
   */
  function parseEase(ease: EaseString | EaseFunction): EaseFunction;
  function parseEase(): EaseMap;

  /**
   * Returns a function that acts as a simpler alternative of gsap.set() that is more performant but less versatile.
   *
   * ```js
   * let setX = gsap.quickSetter("#id", "x", "px");
   * 
   * // later
   * setX(100);
   * ```
   *
   * @param {TweenTarget} targets
   * @param {string} property
   * @param {string} [unit]
   * @returns {Function} Setter function
   * @memberof gsap
   * @link https://greensock.com/docs/v3/GSAP/gsap.quickSetter()
   */
  function quickSetter(targets: TweenTarget, property: string, unit?: string): Function;

  /**
   * Returns a reusable function that performantly redirects a specific property to a new value, restarting the animation each time you feed in a new number.
   *
   * ```js
   * let xTo = gsap.quickTo("#id", "x", {duration: 0.8, ease: "power3"});
   *
   * // later
   * xTo(100);
   * ```
   *
   * @param {TweenTarget} target
   * @param {string} property
   * @param {TweenVars} vars
   * @returns {QuickToFunc} Setter function
   * @memberof gsap
   * @link https://greensock.com/docs/v3/GSAP/gsap.quickTo()
   */
  function quickTo(target: TweenTarget, property: string, vars?: TweenVars): QuickToFunc;

  /**
   * Register custom easing functions with GSAP, giving it a name so it can be referenced in any tweens.
   *
   * ```js
   * gsap.registerEase("myEaseName", function(progress) {
   *   return progress; //linear
   * });
   * ```
   *
   * @param {string} name
   * @param {EaseFunction} ease
   * @memberof gsap
   * @link https://greensock.com/docs/v3/GSAP/gsap.registerEase()
   */
  function registerEase(name: string, ease: EaseFunction): void;

  // TODO: Create interface for effect
  /**
   * Registers custom effects (named tweens) for reuse with optional arguments.
   *
   * ```js
   * // register the effect with GSAP:
   * gsap.registerEffect({
   *   name: "fade",
   *   effect: (targets, config) => {
   *     return gsap.to(targets, {duration: config.duration, opacity: 0});
   *   },
   *   defaults: {duration: 2}, //defaults get applied to any "config" object passed to the effect
   *   extendTimeline: true, //now you can call the effect directly on any GSAP timeline to have the result immediately inserted in the position you define (default is sequenced at the end)
   * });
   *
   * // now we can use it like this:
   * gsap.effects.fade(".box");
   * // or
   * tl.fade(".box", {duration: 3})
   * ```
   *
   * @param {object} effect
   * @memberof gsap
   * @link https://greensock.com/docs/v3/GSAP/gsap.registerEffect()
   */
  function registerEffect(effect: object): void;
  
  /**
   * Installs the specified GSAP plugins, provided they have been loaded already.
   * 
   * ```js
   * gsap.registerPlugin(MorphSVPlugin, MotionPathPlugin);
   * ```
   *
   * @param {RegisterablePlugins[]} args
   * @memberof gsap
   * @link https://greensock.com/docs/v3/GSAP/gsap.registerPlugin()
   */
  function registerPlugin(...args: object[]): void;
  
  /**
   * Immediately sets properties of the target(s) to the properties specified.
   *
   * ```js
   * gsap.set(".class", {x: 100, y: 50, opacity: 0});
   * ```
   *
   * @param {TweenTarget} targets
   * @param {TweenVars} vars
   * @returns {Tween} Tween instance
   * @memberof gsap
   * @link https://greensock.com/docs/v3/GSAP/gsap.set()
   */
  function set(targets: TweenTarget, vars: TweenVars): core.Tween;

  /**
   * Creates a new timeline, used to compose sequences of tweens.
   *
   * @param {TimelineVars} [vars]
   * @returns {Timeline} Timeline instance
   * @memberof gsap
   * @link https://greensock.com/docs/v3/GSAP/gsap.timeline()
   */
  function timeline(vars?: TimelineVars): core.Timeline;

  /**
   * Creates a tween going TO the given values.
   *
   * ```js
   * gsap.to(".class", {x: 100});
   * ```
   *
   * @param {TweenTarget} targets
   * @param {TweenVars} vars
   * @returns {Tween} Tween instance
   * @memberof gsap
   * @link https://greensock.com/docs/v3/GSAP/gsap.to()
   */
  function to(targets: TweenTarget, vars: TweenVars): core.Tween;

  /**
   * Manually update the root (global) timeline. Make sure to unhook GSAP's default ticker.
   *
   * ```js
   * // unhooks the GSAP ticker
   * gsap.ticker.remove(gsap.updateRoot);
   * 
   * // sets the root time to 20 seconds manually
   * gsap.updateRoot(20);
   * ```
   *
   * @param {number} time
   * @returns {void} Void
   * @memberof gsap
   * @link https://greensock.com/docs/v3/GSAP/gsap.updateRoot()
   */
  function updateRoot(time: number): void;  
}

// TODO: Move to files where declared
/**
 * @deprecated since 3.0.0
 * @link https://greensock.com/3-release-notes/
 */
declare class TweenLite extends gsap.core.Tween {}

/**
 * @deprecated since 3.0.0
 * @link https://greensock.com/3-release-notes/
 */
declare class TweenMax extends gsap.core.Tween {}

/**
 * @deprecated since 3.0.0
 * @link https://greensock.com/3-release-notes/
 */
declare class TimelineLite extends gsap.core.Timeline {}

/**
 * @deprecated since 3.0.0
 * @link https://greensock.com/3-release-notes/
 */
declare class TimelineMax extends gsap.core.Timeline {}

declare module "gsap/gsap-core" {

  const _gsap: typeof gsap;  

  // TODO: Move to files where declared
  /**
   * @deprecated since 3.0.0
   * @link https://greensock.com/3-release-notes/
   */
  export class TweenLite extends gsap.core.Tween {}

  /**
   * @deprecated since 3.0.0
   * @link https://greensock.com/3-release-notes/
   */
  export class TweenMax extends gsap.core.Tween {}

  /**
   * @deprecated since 3.0.0
   * @link https://greensock.com/3-release-notes/
   */
  export class TimelineLite extends gsap.core.Timeline {}

  /**
   * @deprecated since 3.0.0
   * @link https://greensock.com/3-release-notes/
   */
  export class TimelineMax extends gsap.core.Timeline {}

  export {
    _gsap as gsap,
    _gsap as default
  }
}

declare module "gsap/src/gsap-core" {
  export * from "gsap/gsap-core";
  export { gsap as default } from "gsap/gsap-core";
}

declare module "gsap-trial/gsap-core" {
  export * from "gsap/gsap-core";
  export { gsap as default } from "gsap/gsap-core";
}

declare module "gsap-trial/src/gsap-core" {
  export * from "gsap/gsap-core";
  export { gsap as default } from "gsap/gsap-core";
}
