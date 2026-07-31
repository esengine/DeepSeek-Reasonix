declare class Draggable {

  static version: string;
  static zIndex: number;

  readonly autoScroll: number;
  readonly deltaX: number;
  readonly deltaY: number;
  readonly endRotation: number;
  readonly endX: number;
  readonly endY: number;
  readonly isDragging: boolean;
  readonly isPressed: boolean;
  readonly isThrowing: boolean;
  readonly lockAxis: boolean;
  readonly maxRotation: number;
  readonly maxX: number;
  readonly maxY: number;
  readonly minX: number;
  readonly minY: number;
  readonly minRotation: number;
  readonly pointerEvent: TouchEvent | PointerEvent;
  readonly pointerX: number;
  readonly pointerY: number;
  readonly rotation: number;
  readonly scrollProxy: any; // TODO: Create interface
  readonly startX: number;
  readonly startY: number;
  readonly target: HTMLElement | SVGElement;  
  readonly tween: gsap.core.Tween;  
  readonly vars: Draggable.Vars;
  readonly x: number;
  readonly y: number;

  constructor(target: gsap.DOMTarget, vars?: Draggable.Vars);

  /**
   * A more flexible way to create Draggable instances than the constructor.
   * 
   * ```js
   * Draggable.create(".myClass", {type: "x,y"});
   * ```
   *
   * @param {gsap.DOMTarget} target
   * @param {Draggable.Vars} [vars]
   * @returns {Draggable[]} Array of Draggables
   * @memberof Draggable
   * @link https://greensock.com/docs/v3/Plugins/Draggable/static.create()
   */
  static create(target: gsap.DOMTarget, vars?: Draggable.Vars): Draggable[];

  /**
   * Get the Draggable instance that's associated with a particular DOM element.
   * 
   * ```js
   * var draggable = Draggable.get("#myId");
   * ```
   *
   * @param {gsap.DOMTarget} target
   * @returns {Draggable} The Draggable
   * @memberof Draggable
   * @link https://greensock.com/docs/v3/Plugins/Draggable/static.get()
   */
  static get(target: gsap.DOMTarget): Draggable;

  /**
   * Test whether or not the target element overlaps with a particular element or the mouse position, optionally including a threshold.
   * 
   * ```js
   * Draggable.hitTest(element1, element2, 20)
   * ```
   *
   * @param {Draggable.TestObject} testObject1
   * @param {Draggable.TestObject} testObject2
   * @param {number | string} [threshold]
   * @returns {boolean} If the hit threshhold is met or not
   * @memberof Draggable
   * @link https://greensock.com/docs/v3/Plugins/Draggable/static.hitTest()
   */
  static hitTest(testObject1: Draggable.TestObject, testObject2: Draggable.TestObject, threshold?: number | string): boolean;

  /**
   * Returns the time (in seconds) that has elapsed since the last drag ended.
   * 
   * ```js
   * Draggable.timeSinceDrag();
   * ```
   *
   * @returns {number} The time since the last drag ended
   * @memberof Draggable
   * @link https://greensock.com/docs/v3/Plugins/Draggable/static.timeSinceDrag()
   */
  static timeSinceDrag(): number;
  

  /**
   * Registers a function that should be called each time a particular type of event occurs.
   * 
   * ```js
   * draggable.addEventListener("press", myPressFunction);
   * ```
   *
   * @param {Draggable.CallbackType} type
   * @param {gsap.Callback} callback
   * @memberof Draggable
   * @link https://greensock.com/docs/v3/Plugins/Draggable/addEventListener()
   */
  addEventListener(type: Draggable.CallbackType, callback: gsap.Callback): void;

  /**
   * Registers a function that should be called each time a particular type of event occurs.
   * 
   * ```js
   * draggable.applyBounds("#dragContainer");
   * draggable.applyBounds({top: 100, left: 0, width: 1000, height: 800});
   * draggable.applyBounds({minX: 10, maxX: 300, minY: 50, maxY: 500});
   * draggable.applyBounds({minRotation: 0, maxRotation: 270});
   * ```
   *
   * @param {gsap.DOMTarget | Draggable.BoundsMinMax | Draggable.BoundsRectangle | Draggable.BoundsRotation} bounds
   * @memberof Draggable
   * @link https://greensock.com/docs/v3/Plugins/Draggable/applyBounds()
   */
  applyBounds(bounds: gsap.DOMTarget | Draggable.BoundsMinMax | Draggable.BoundsRectangle | Draggable.BoundsRotation): void;

  /**
   * Disables the Draggable instance so that it cannot be dragged anymore.
   * 
   * ```js
   * draggable.disable();
   * ```
   *
   * @param {string | object} [type]
   * @returns {Draggable} The Draggable instance
   * @memberof Draggable
   * @link https://greensock.com/docs/v3/Plugins/Draggable/disable()
   */
  disable(type?: string | object): this;

  dispatchEvent(type: Draggable.CallbackType): boolean;

  /**
   * Enables the Draggable instance so that it can be dragged.
   * 
   * ```js
   * draggable.enable();
   * ```
   *
   * @param {string | object} [type]
   * @returns {Draggable} The Draggable instance
   * @memberof Draggable
   * @link https://greensock.com/docs/v3/Plugins/Draggable/enable()
   */
  enable(type?: string | object): this;

  /**
   * Sets the enabled state of the Draggable.
   *
   * ```js
   * draggable.enabled(true);
   * ```
   *
   * @param {boolean} value
   * @returns {Draggable} The Draggable
   * @memberof Draggable
   * @link https://greensock.com/docs/v3/Plugins/Draggable/enabled()
   */
  enabled(value: boolean): this;
  /**
   * Gets the enabled state of the Draggable.
   *
   * ```js
   * draggable.enabled();
   * ```
   *
   * @returns {boolean} The enabled state
   * @memberof Draggable
   * @link https://greensock.com/docs/v3/Plugins/Draggable/enabled()
   */
  enabled(): boolean;

  /**
   * Force the Draggable to immediately stop interactively dragging. 
   * You must pass it the original mouse or touch event that initiated the stop.
   *
   * ```js
   * draggable.endDrag(e);
   * ```
   *
   * @param {Event} event
   * @memberof Draggable
   * @link https://greensock.com/docs/v3/Plugins/Draggable/endDrag()
   */
  endDrag(event: Event): void;

  /**
   * Returns the direction, velocity, or proximity to another object.
   *
   * ```js
   * draggable.getDirection("start");
   * draggable.getDirection("velocity");
   * draggable.getDirection(refElem);
   * ```
   *
   * @param {"start" | "velocity" | gsap.DOMTarget} from
   * @returns {Draggable.Direction} The direction
   * @memberof Draggable
   * @link https://greensock.com/docs/v3/Plugins/Draggable/getDirection()
   */
  getDirection(from: "start" | "velocity" | gsap.DOMTarget): Draggable.Direction;

  /**
   * Test whether or not the target element overlaps with a particular element or the mouse position, optionally including a threshold.
   * 
   * ```js
   * draggable.hitTest(otherElem, 20);
   * ```
   *
   * @param {Draggable.TestObject} testObject
   * @param {number | string} [threshold]
   * @returns {boolean} If the hit threshhold is met or not
   * @memberof Draggable
   * @link https://greensock.com/docs/v3/Plugins/Draggable/static.hitTest()
   */
  hitTest(testObject: Draggable.TestObject, threshold?: number | string): boolean;

  /**
   * Disables the Draggable instance and frees it for garbage collection
   * so that it cannot be dragged anymore.
   * 
   * ```js
   * draggable.kill();
   * ```
   * 
   * @returns {Draggable} The Draggable instance
   * @memberof Draggable
   * @link https://greensock.com/docs/v3/Plugins/Draggable/kill()
   */
  kill(): this;

  removeEventListener(type: Draggable.CallbackType, callback: gsap.Callback): void;

  /**
   * Force the Draggable to start interactively dragging. 
   * You must pass it the original mouse or touch event that initiated the start.
   *
   * ```js
   * draggable.startDrag(e);
   * ```
   *
   * @param {Event} event
   * @param {boolean} align
   * @memberof Draggable
   * @link https://greensock.com/docs/v3/Plugins/Draggable/startDrag()
   */
  startDrag(event: Event, align?: boolean): void;

  /**
   * Returns the time (in seconds) that has elapsed since the last drag ended.
   * 
   * ```js
   * draggable.timeSinceDrag();
   * ```
   *
   * @returns {number} The time since the last drag ended
   * @memberof Draggable
   * @link https://greensock.com/docs/v3/Plugins/Draggable/static.timeSinceDrag()
   */
  timeSinceDrag(): number;

  /**
   * Updates the Draggable's x/y properties to reflect the target element's current position.
   * 
   * ```js
   * Draggable.update();
   * ```
   *
   * @param {boolean} [applyBounds]
   * @param {boolean} [sticky]
   * @returns {Draggable} The Draggable instance
   * @memberof Draggable
   * @link https://greensock.com/docs/v3/Plugins/Draggable/update()
   */
  update(applyBounds?: boolean, sticky?: boolean): this;
}

declare namespace Draggable {

  type CallbackType = 
    | "click" 
    | "drag" 
    | "dragend" 
    | "dragstart" 
    | "move" 
    | "press" 
    | "release"
    | "throwcomplete"
    | "throwupdate";

  type Direction = 
    | "down"
    | "left"
    | "left-down"
    | "left-up"
    | "up"
    | "right"
    | "right-down"
    | "right-up";

  type DraggableType = 
    | "left"
    | "left,top"
    | "rotation"
    | "scroll"
    | "scrollLeft"
    | "scrollTop"
    | "top"
    | "top,left"
    | "x"
    | "x,y"
    | "y"
    | "y,x";

  type SnapValue = number[] | ((value: number) => number);
  type TestObject = gsap.DOMTarget | Event | Rectangle;

  interface BoundsMinMax {
    minX?: number;
    minY?: number;
    maxX?: number;
    maxY?: number;
  }

  interface BoundsRectangle {
    height?: number;
    left?: number;
    top?: number;
    width?: number;
  }

  interface BoundsRotation {
    minRotation?: number;
    maxRotation?: number;
  }

  interface Rectangle {
    bottom: number;
    left: number;
    right: number;
    top: number;
  }

  interface SnapObject {
    left?: SnapValue;
    points?: gsap.Point2D[] | ((point: gsap.Point2D) => gsap.Point2D);
    radius?: number;
    rotation?: SnapValue;
    top?: SnapValue;
    x?: SnapValue;
    y?: SnapValue;
  }

  interface Vars {
    [key: string]: any;
    activeCursor?: string;
    allowContextMenu?: boolean;
    allowEventDefault?: boolean;
    allowNativeTouchScrolling?: boolean;
    autoScroll?: number;
    bounds?: gsap.DOMTarget | BoundsMinMax | BoundsRectangle | BoundsRotation;
    callbackScope?: object;
    clickableTest?: (this: Draggable, element: HTMLElement | SVGElement) => void;
    cursor?: string;
    dragClickables?: boolean;
    dragResistance?: number;
    edgeResistance?: number;
    force3D?: "auto" | boolean;    
    inertia?: boolean | gsap.InertiaVars;
    liveSnap?: boolean | SnapValue | SnapObject;
    lockAxis?: boolean;
    maxDuration?: number;
    minDuration?: number;
    minimumMovement?: number;
    onClick?: gsap.Callback;
    onClickParams?: any[];
    onDrag?: gsap.Callback;
    onDragParams?: any[];
    onDragStart?: gsap.Callback;
    onDragStartParams?: any[];
    onDragEnd?: gsap.Callback;
    onDragEndParams?: any[];
    onLockAxis?: (this: Draggable, event: Event) => void;
    onMove?: gsap.Callback;
    onMoveParams?: any[];
    onPress?: gsap.Callback;
    onPressParams?: any[];
    onPressInit?: gsap.Callback;
    onPressInitParams?: any[];
    onRelease?: gsap.Callback;
    onReleaseParams?: any[];
    onThrowComplete?: gsap.Callback;
    onThrowCompleteParams?: any[];
    onThrowUpdate?: gsap.Callback;
    onThrowUpdateParams?: any[];    
    overshootTolerance?: number;
    resistance?: number;
    snap?: SnapValue | SnapObject;    
    throwProps?: boolean | gsap.InertiaVars;
    throwResistance?: number;
    trigger?: gsap.DOMTarget;
    type?: DraggableType;
    zIndexBoost?: boolean;
  }
}

declare module "gsap/Draggable" {
  class _Draggable extends Draggable {}
  export {
    _Draggable as Draggable,
    _Draggable as default
  }
}

declare module "gsap/src/Draggable" {
  export * from "gsap/Draggable";
  export { Draggable as default } from "gsap/Draggable";
}

declare module "gsap/dist/Draggable" {
  export * from "gsap/Draggable";
  export { Draggable as default } from "gsap/Draggable";
}

declare module "gsap/all" {
  export * from "gsap/Draggable";
}

declare module "gsap-trial/Draggable" {
  export * from "gsap/Draggable";
  export { Draggable as default } from "gsap/Draggable";
}

declare module "gsap-trial/src/Draggable" {
  export * from "gsap/Draggable";
  export { Draggable as default } from "gsap/Draggable";
}

declare module "gsap-trial/dist/Draggable" {
  export * from "gsap/Draggable";
  export { Draggable as default } from "gsap/Draggable";
}

declare module "gsap-trial/all" {
  export * from "gsap/Draggable";
}
