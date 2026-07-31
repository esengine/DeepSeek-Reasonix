declare class Flip {

  static readonly version: string;

  /**
   * Gets the FlipBatch associated with the provided id ("default" by default); if one hasn't be created/registered yet, a new one is returned and registered.
   *
   * ```js
   * let batch = Flip.batch("id");
   * ```
   *
   * @static
   * @param {string} [id]
   * @returns {FlipBatch} the FlipBatch (if one isn't registered, a new one is created/registered and returned)
   * @memberof Flip
   * @link https://greensock.com/docs/v3/Plugins/Flip/static.batch()
   */
  static batch(id?: string): FlipBatch;
  
  /**
   * Gets the matrix to convert points from one element's local coordinates into a
   * different element's local coordinate system.
   *
   * ```js
   * Flip.convertCoordinates(fromElement, toElement);
   * ```
   *
   * @static
   * @param {Element} fromElement
   * @param {Element} toElement
   * @returns {gsap.plugins.Matrix2D} A matrix to convert from one element's coordinate system to another's
   * @memberof Flip
   */
  static convertCoordinates(fromElement: Element, toElement: Element): gsap.plugins.Matrix2D;
    
  /**
   * Converts a point from one element's local coordinates into a
   * different element's local coordinate system.
   *
   * ```js
   * Flip.convertCoordinates(fromElement, toElement, point);
   * ```
   *
   * @static
   * @param {Element} fromElement
   * @param {Element} toElement
   * @param {gsap.Point2D} point
   * @returns {gsap.Point2D} A point to convert from one element's coordinate system to another's
   * @memberof Flip
   */
  static convertCoordinates(fromElement: Element, toElement: Element, point: gsap.Point2D): gsap.Point2D;
   
  /**
   * Changes the x/y/rotation/skewX transforms (and width/height or scaleX/scaleY) to fit one element exactly into the the position/size/rotation of another element.
   * 
   * ```js
   * Flip.fit("#el1", "#el2", {
   *   scale: true, 
   *   absolute: true, 
   *   duration: 1, 
   *   ease: "power2"
   * });
   * ```
   *
   * @static
   * @param {gsap.DOMTarget} fromElement
   * @param {(gsap.DOMTarget | Flip.FlipState)} toElement
   * @param {Flip.FitVars} [vars]
   * @returns {(gsap.core.Tween | object | null)}
   * @memberof Flip
   */
  static fit(fromElement: gsap.DOMTarget, toElement: gsap.DOMTarget | Flip.FlipState, vars?: Flip.FitVars): gsap.core.Tween | object | null;

  /**
   * Animates the targets from the provided state to their current state (position/size).
   * 
   * ```js
   * Flip.from(state, {
   *   duration: 1,
   *   ease: "power1.inOut",
   *   stagger: 0.1,
   *   onComplete: () => console.log("done")
   * });
   * ```
   *
   * @static
   * @param {Flip.FlipState} state
   * @param {Flip.FromToVars} [vars]
   * @returns {gsap.core.Timeline} Flip timeline
   * @memberof Flip
   * @link https://greensock.com/docs/v3/Plugins/Flip/static.from()
   */
  static from(state: Flip.FlipState, vars?: Flip.FromToVars): gsap.core.Timeline;

  /**
   * Captures information about the current state of the targets so that they can be flipped later.
   * 
   * ```js
   * let state = Flip.getState(".my-class, .another-class", {props: "backgroundColor,color", simple: true});
   * ```
   *
   * @static
   * @param {gsap.DOMTarget} targets
   * @param {(Flip.FlipStateVars | string)} [vars]
   * @returns {Flip.FlipState} The resulting state object
   * @memberof Flip
   * @link https://greensock.com/docs/v3/Plugins/Flip/static.getState()
   */
  static getState(targets: gsap.DOMTarget, vars?: Flip.FlipStateVars | string): Flip.FlipState;

  /**
   * Gets the timeline for the most recently-created flip animation associated with the provided element
   *
   * ```js
   * let tl = Flip.getByTarget("#elementID");
   * ```
   *
   * @static
   * @param {Element | string} target
   * @returns {core.Timeline | null} The timeline for the most recently-created flip animation associated with the provided element
   * @memberof Flip
   */
  static getByTarget(target: Element | string): gsap.core.Timeline | null;

  /**
   * Determines whether or not a particular element is actively flipping (has an active flip animation)
   *
   * ```js
   * if (!Flip.isFlipping("#elementID")) {
   *   // do stuff
   * }
   * ```
   * 
   * @static
   * @param {gsap.DOMTarget} target
   * @returns {boolean} whether or not the target element is actively flipping
   * @memberof Flip
   * @link https://greensock.com/docs/v3/Plugins/Flip/static.isFlipping()
   */
  static isFlipping(target: gsap.DOMTarget): boolean;

  /**
   * Immediately kills any Flip animations that are running on the target(s) provided, completing them as well (unless "complete" parameter is explicitly false).
   *
   * ```js
   * Flip.killFlipsOf(".box");
   * ```
   *
   * @static
   * @param {gsap.DOMTarget} targets
   * @param {boolean} complete
   * @memberof Flip
   * @link https://greensock.com/docs/v3/Plugins/Flip/static.killFlipsOf()
   */
  static killFlipsOf(targets: gsap.DOMTarget, complete?: boolean): void;

  /**
   * Sets all of the provided target elements to position: absolute while retaining their current positioning.
   *
   * ```js
   * Flip.makeAbsolute(".my-class");
   * ```
   *
   * @static
   * @param {Element | string | null | ArrayLike<Element | string>} targets
   * @returns {Element[]} An Array containing the Elements that were affected
   * @memberof Flip
   * @link https://greensock.com/docs/v3/Plugins/Flip/static.makeAbsolute()
   */
  static makeAbsolute(targets: gsap.DOMTarget | Flip.FlipState[]): Element[];

  /**
   * Animates the targets from the current state to the provided state.
   * 
   * ```js
   * Flip.to(state, {
   *   duration: 1,
   *   ease: "power1.inOut",
   *   stagger: 0.1,
   *   onComplete: () => console.log("done")
   * });
   * ```
   *
   * @static
   * @param {Flip.FlipState} state
   * @param {Flip.FromToVars} [vars]
   * @returns {gsap.core.Timeline} Flip timeline
   * @memberof Flip
   * @link https://greensock.com/docs/v3/Plugins/Flip/static.to()
   */
  static to(state: Flip.FlipState, vars?: Flip.FromToVars): gsap.core.Timeline;

  /**
   * Registers Flip with gsap
   *
   * @static
   * @param {typeof gsap} core
   * @memberof Flip
   */
  static register(core: typeof gsap): void;
}

declare namespace Flip {

  type OverrideProps<M, N> = { [P in keyof M]: P extends keyof N ? N[P] : M[P] };

  type EnterOrLeaveCallback = (elements: Element[]) => any;
  type SpinFunction = (index: number, element: Element, targets: Element[]) => number;
  type LoadStateFunction = (load: Function) => any;
  type BatchActionFunction = (self: FlipBatchAction) => any;

  interface TweenVarOverrides {
    scale?: boolean;
  }

  interface AnimationVars extends OverrideProps<gsap.TweenVars, TweenVarOverrides> {
    absolute?: boolean | gsap.DOMTarget;
    simple?: boolean;
    props?: string;
  }

  interface FlipStateVars {
    simple?: boolean;
    props?: string;
  }

  interface StateCompare {
    changed: Element[];
    unchanged: Element[];
    enter: Element[];
    leave: Element[];
  }

  interface FromToVars extends AnimationVars {
    absoluteOnLeave?: boolean;
    prune?: boolean;
    fade?: boolean;
    nested?: boolean;
    onEnter?: EnterOrLeaveCallback;
    onLeave?: EnterOrLeaveCallback;
    spin?: number | boolean | SpinFunction;
    targets?: gsap.DOMTarget;
    toggleClass?: string;
    zIndex?: number;
  }

  interface FitReturnVars {
    width?: number;
    height?: number;
    rotation: number;
    scaleX?: number;
    scaleY?: number;
    skewX: number;
    x: number;
    y: number;
    [key: string]: any;
  }
  interface FitVars extends AnimationVars {
    fitChild?: gsap.DOMTarget;
    getVars?: boolean;
  }

  interface BatchActionConfig {
    getState?: BatchActionFunction;
    loadState?: LoadStateFunction;
    setState?: BatchActionFunction;
    animate?: BatchActionFunction;
    onEnter?: EnterOrLeaveCallback;
    onLeave?: EnterOrLeaveCallback;
    onStart?: BatchActionFunction;
    onComplete?: BatchActionFunction;
    once?: boolean;
  }
  
  class ElementState {
    readonly bounds: DOMRect;
    readonly cache: object;
    readonly display: string;
    readonly element: Element;
    readonly getProp: Function;
    readonly height: number;
    readonly id: string;
    readonly isVisible: boolean;
    readonly matrix: gsap.plugins.Matrix2D;
    readonly opacity: number;
    readonly parent: Element | null;
    readonly position: string;
    readonly rotation: number;
    readonly scaleX: number;
    readonly scaleY: number;
    readonly simple: boolean;
    readonly skewX: number;
    readonly width: number;
    readonly x: number;
    readonly y: number;

    isDifferent(elState: ElementState): boolean;
  }

  class FlipState {
    readonly alt: object;
    readonly elementStates: ElementState[];
    readonly idLookup: object;
    readonly props: string | null;
    readonly simple: boolean;
    readonly targets: Element[];

    add(state: FlipState): FlipState;
    clear(): FlipState;
    compare(state: FlipState): StateCompare;
    update(soft?: boolean): FlipState;
    fit(state: FlipState, scale?: boolean, nested?: boolean): this;
    recordInlineStyles(): void;
    interrupt(soft?: boolean): void;
    getProperty(element: string | Element, property: string): any;
    getElementState(element: Element): ElementState;
    makeAbsolute(): Element[];
  }
}

declare class FlipBatchAction {
  readonly batch: FlipBatch;
  readonly state: any;
  readonly states: Flip.FlipState[];
  readonly timeline: gsap.core.Timeline;
  readonly targets: any;
  readonly vars: Flip.BatchActionConfig;

  /**
   * Searches the state objects that were captured inside the action's getState() on its most recent call, and returns the first one it finds that matches the provided data-flip-id value.
   *
   * ```js
   * let state = action.getStateById("box1");
   * ```
   * @param {string} id
   * @memberof FlipBatchAction
   */
  getStateById(id: string): Flip.FlipState | null;

  /**
   * Kills the batch action, removing it from its batch.
   *
   * @memberof FlipBatchAction
   */
  kill(): FlipBatchAction;
}

declare class FlipBatch {
  readonly actions: FlipBatchAction[];
  readonly state: Flip.FlipState;
  readonly timeline: gsap.core.Timeline;
  readonly id: string;
  data: any;

  /**
   * Adds a Flip action to the batch so that MULTIPLE Flips can be combined and run each of their steps together (getState(), loadState(), setState(), animate())
   *
   * ```js
   * batch.add({
   *     getState: self => Flip.getState(targets),
   *     loadState: done => done(),
   *     setState: self => app.classList.toggle("active"),
   *     animate: self => {
   *       Flip.from(self.state, {ease: "power1.inOut"});
   *     },
   *     onStart: startCallback,
   *     onComplete: completeCallback,
   *     onEnter: elements => console.log("entering", elements),
   *     onLeave: elements => console.log("leaving", elements),
   *     once: true
   * });
   * ```
   *
   * @param {BatchActionConfig | Function} config
   * @returns {FlipBatchAction} A FlipBatchAction
   * @memberof FlipBatch
   */
  add(config: Flip.BatchActionConfig | Function): FlipBatchAction;


  /**
   * Flushes the batch.state (merged) object and removes all actions (unless stateOnly parameter is true)
   *
   * ```js
   * batch.clear(true);
   * ```
   *
   * @param {boolean} stateOnly
   * @returns {FlipBatch} self
   * @memberof FlipBatch
   */
  clear(stateOnly?: boolean): FlipBatch;


  /**
   * Calls getState() on all actions in this batch (any that are defined at least), optionally merging the results into batch.state
   *
   * ```js
   * batch.getState(true);
   * ```
   *
   * @param {boolean} merge (false by default)
   * @returns {FlipBatch} self
   * @memberof FlipBatch
   */
  getState(merge?: boolean): FlipBatch;

  /**
   * Searches the state objects that were captured inside ANY of this batch actions' most recent getState() call, and returns the first one it finds that matches the provided data-flip-id value.
   *
   * ```js
   * let state = batch.getStateById("box1");
   * ```
   * @param {string} id
   * @memberof FlipBatch
   */
  getStateById(id: string): Flip.FlipState | null;

  /**
   * Kills the batch, unregistering it internally and making it available for garbage collection. Also clears all actions and flushes the batch.state (merged) object.
   *
   * @memberof FlipBatch
   */
  kill(): FlipBatchAction;

  /**
   * Removes a particular action from the batch.
   *
   * ```js
   * batch.remove(action);
   * ```
   *
   * @param {FlipBatchAction} action
   * @returns {FlipBatch} self
   * @memberof FlipBatch
   */
  remove(action: FlipBatchAction): FlipBatch;

  /**
   * Executes all actions in the batch in the proper order: getState() (unless skipGetState is true), loadState(), setState(), and animate()
   *
   * ```js
   * batch.run(true);
   * ```
   *
   * @param {boolean} skipGetState
   * @param {boolean} merge
   * @returns {FlipBatch} self
   * @memberof FlipBatch
   */
  run(skipGetState?: boolean, merge?: boolean): FlipBatch;

}

declare namespace gsap {

  /**
   * @deprecated since 3.7.0
   * @see Flip.ElementState
   */
  type ElementState = any;

  /**
   * @deprecated since 3.7.0
   * @see Flip.EnterOrLeaveCallback
   */
  type EnterOrLeaveCallback = Flip.EnterOrLeaveCallback;

  /**
   * @deprecated since 3.7.0
   * @see Flip.FitVars
   */
  type FitVars = Flip.FitVars;

   /**
    * @deprecated since 3.7.0
    * @see Flip.FitReturnVars
    */
  type FitReturnVars = Flip.FitReturnVars;

  /**
   * @deprecated since 3.7.0
   * @see Flip
   */
  type Flip = any;

  /**
   * @deprecated since 3.7.0
   * @see Flip.FlipState
   */
  type FlipState = any;

  /**
   * @deprecated since 3.7.0
   * @see Flip.FlipStateVars
   */
  type FlipStateVars = Flip.FlipStateVars;

  /**
   * @deprecated since 3.7.0
   * @see Flip.FromToVars
   */
  type FlipToFromVars = Flip.FromToVars;  

  /**
   * @deprecated since 3.7.0
   * @see Flip.SpinFunction
   */
  type SpinFunction = Flip.SpinFunction;
}

declare module "gsap/Flip" {
  class _Flip extends Flip { }
  export {
    _Flip as Flip,
    _Flip as default
  }
}

declare module "gsap/dist/Flip" {
  export * from "gsap/Flip";
  export { Flip as default } from "gsap/Flip";
}

declare module "gsap/src/Flip" {
  export * from "gsap/Flip";
  export { Flip as default } from "gsap/Flip";
}

declare module "gsap/all" {
  export * from "gsap/Flip";
}

declare module "gsap-trial/Flip" {
  export * from "gsap/Flip";
  export { Flip as default } from "gsap/Flip";
}

declare module "gsap-trial/dist/Flip" {
  export * from "gsap/Flip";
  export { Flip as default } from "gsap/Flip";
}

declare module "gsap-trial/src/Flip" {
  export * from "gsap/Flip";
  export { Flip as default } from "gsap/Flip";
}

declare module "gsap-trial/all" {
  export * from "gsap/Flip";
}
