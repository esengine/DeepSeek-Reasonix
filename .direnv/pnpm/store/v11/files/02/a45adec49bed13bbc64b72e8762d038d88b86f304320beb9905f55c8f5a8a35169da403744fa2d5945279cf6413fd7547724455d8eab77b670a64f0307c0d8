declare class Observer {
  static readonly isTouch: number;
  static readonly eventTypes: string[];
  static readonly version: string;

  readonly deltaX: number;
  readonly deltaY: number;
  readonly event: Event;
  readonly isDragging: boolean;
  readonly isEnabled: boolean;
  readonly isPressed: boolean;
  readonly startX?: number;
  readonly startY?: number;
  readonly target: Element;
  readonly vars: Observer.ObserverVars;
  readonly velocityX: number;
  readonly velocityY: number;
  readonly x?: number;
  readonly y?: number;
  readonly axis?: string | null;

  /**
   * Creates a new Observer
   *
   * ```js
   * Observer.create({
   *   target: "#id",
   *   onUp: () => console.log("up"),
   *   onDown: () => console.log("down"),
   *   onPress: () => console.log("press")
   * });
   * ```
   *
   * @static
   * @param {Observer.ObserverVars} vars
   * @returns {Observer} The Observer
   * @memberof Observer
   * @link https://greensock.com/docs/v3/Plugins/Observer/static.create()
   */
  static create(vars: Observer.ObserverVars): Observer;

  /**
   * Gets all Observers (that haven't been killed)
   *
   * ```js
   * Observer.getAll().forEach(o => o.kill());
   * ```
   *
   * @static
   * @returns {Observer[]} An Array of Observers
   * @memberof Observer
   * @link https://greensock.com/docs/v3/Plugins/Observer/static.getAll()
   */
  static getAll(): Observer[];

  /**
   * Gets the observer with the id provided.
   *
   * ```js
   * let o = Observer.getById("my-id");
   * ```
   *
   * @static
   * @param {string} id
   * @returns {Observer | undefined} The Observer with the supplied id (if one exists)
   * @memberof Observer
   * @link https://greensock.com/docs/v3/Plugins/Observer/static.getAll()
   */
  static getById(id: string): Observer | undefined;

  /**
   * Disables a Observer instance.
   *
   * ```js
   * observer.disable();
   * ```
   */
  disable(): void;

  /**
   * Re-enables a disabled Observer instance.
   *
   * ```js
   * observer.enable();
   * ```
   */
  enable(): this;

  /**
   * Kills a Observer instance (same as disabling, but typically permanent).
   *
   * ```js
   * observer.kill();
   * ```
   */
  kill(): void;

  /**
   * Gets the horizontal scroll position of the target (typically scrollLeft).
   *
   * ```js
   * observer.scrollX();
   * ```
   *
   * @returns {number} The horizontal scroll position of the target
   */
  scrollX(): number;

  /**
   * Sets the horizontal scroll position of the target (typically scrollTop).
   *
   * ```js
   * observer.scrollX(100);
   * ```
   *
   * @param {number} position
   */
  scrollX(position: number): void;

  /**
   * Gets the vertical scroll position of the target (typically scrollTop).
   *
   * ```js
   * observer.scrollY();
   * ```
   *
   * @returns {number} The vertical scroll position of the target
   */
  scrollY(): number;

  /**
   * Sets the vertical scroll position of the target (typically scrollTop).
   *
   * ```js
   * observer.scrollY(100);
   * ```
   *
   * @param {number} position
   */
  scrollY(position: number): void;

}

declare namespace Observer {

  type ObserverCallback = (self: Observer) => any;
  type IgnoreCheckCallback = (event: Event, isTouchOrPointer: boolean) => boolean;

  interface ObserverVars {
    allowClicks?: boolean;
    capture?: boolean;
    debounce?: boolean;
    dragMinimum?: number;
    event?: Event;
    id?: string;
    ignore?: gsap.DOMTarget;
    ignoreCheck?: IgnoreCheckCallback;
    lineHeight?: number;
    lockAxis?: boolean;
    onLockAxis?: ObserverCallback;
    onDown?: ObserverCallback;
    onUp?: ObserverCallback;
    onLeft?: ObserverCallback;
    onRight?: ObserverCallback;
    onDisable?: ObserverCallback;
    onDrag?: ObserverCallback;
    onDragStart?: ObserverCallback;
    onDragEnd?: ObserverCallback;
    onEnable?: ObserverCallback;
    onHover?: ObserverCallback;
    onHoverEnd?: ObserverCallback;
    onToggleY?: ObserverCallback;
    onToggleX?: ObserverCallback;
    onChangeX?: ObserverCallback;
    onChangeY?: ObserverCallback;
    onChange?: ObserverCallback;
    onClick?: ObserverCallback;
    onPress?: ObserverCallback;
    onRelease?: ObserverCallback;
    onMove?: ObserverCallback;
    onWheel?: ObserverCallback;
    onStop?: ObserverCallback;
    onStopDelay?: number;
    preventDefault?: boolean;
    target?: gsap.DOMTarget | Window | Document;
    tolerance?: number;
    type?: string;
    wheelSpeed?: number;
  }
}

declare module "gsap/Observer" {
  class _Observer extends Observer { }
  export {
    _Observer as Observer,
    _Observer as default
  }
}

declare module "gsap/dist/Observer" {
  export * from "gsap/Observer";
  export { Observer as default } from "gsap/Observer";
}

declare module "gsap/src/Observer" {
  export * from "gsap/Observer";
  export { Observer as default } from "gsap/Observer";
}

declare module "gsap/all" {
  export * from "gsap/Observer";
}

declare module "gsap-trial/Observer" {
  export * from "gsap/Observer";
  export { Observer as default } from "gsap/Observer";
}

declare module "gsap-trial/dist/Observer" {
  export * from "gsap/Observer";
  export { Observer as default } from "gsap/Observer";
}

declare module "gsap-trial/src/Observer" {
  export * from "gsap/Observer";
  export { Observer as default } from "gsap/Observer";
}

declare module "gsap-trial/all" {
  export * from "gsap/Observer";
}
