// Type definitions for svg-pan-zoom v3.5.0
// Project: https://github.com/bumbu/svg-pan-zoom
// Definitions by: César Vidril <https://github.com/Yimiprod>
// Definitions: https://github.com/bumbu/svg-pan-zoom

declare namespace SvgPanZoom {
  interface Options {
    /**
     * can be querySelector string or SVGElement (default enabled)
     * @type {string|HTMLElement|SVGElement}
     */
    viewportSelector?: string|HTMLElement|SVGElement;
    /**
     * enable or disable panning (default enabled)
     * @type {boolean}
     */
    panEnabled?: boolean;
    /**
     * insert icons to give user an option in addition to mouse events to control pan/zoom (default disabled)
     * @type {boolean}
     */
    controlIconsEnabled?: boolean;
    /**
     * enable or disable zooming (default enabled)
     * @type {boolean}
     */
    zoomEnabled?: boolean;
    /**
     * enable or disable zooming by double clicking (default enabled)
     * @type {boolean}
     */
    dblClickZoomEnabled?: boolean;
    /**
     * enable or disable zooming by scrolling (default enabled)
     * @type {boolean}
     */
    mouseWheelZoomEnabled?: boolean;
    /**
     * prevent mouse events to bubble up (default enabled)
     * @type {boolean}
     */
    preventMouseEventsDefault?: boolean;
    zoomScaleSensitivity?: number; // Zoom sensitivity (Default 0.2)
    minZoom?: number; // Minimum Zoom level (Default 0.5)
    maxZoom?: number; // Maximum Zoom level  (Default 10)
    fit?: boolean; // enable or disable viewport fit in SVG (default true)
    contain?: boolean; // (default true)
    center?: boolean; // enable or disable viewport centering in SVG (default true)
    refreshRate?: number | "auto"; // (default 'auto')
    beforeZoom?: (oldScale: number, newScale: number) => void | boolean;
    onZoom?: (newScale: number) => void;
    beforePan?: (oldPan: Point, newPan: Point) => void | boolean | PointModifier;
    onPan?: (newPan: Point) => void;
    onUpdatedCTM?: (newCTM: SVGMatrix) => void;
    customEventsHandler?: CustomEventHandler; // (default null)
    eventsListenerElement?: SVGElement; // (default null)
  }

  interface CustomEventHandler {
    init: (options: CustomEventOptions) => void;
    haltEventListeners: string[];
    destroy: Function;
  }

  interface CustomEventOptions {
    svgElement: SVGSVGElement;
    instance: Instance;
  }

  interface Point {
    x: number;
    y: number;
  }

  interface PointModifier {
    x: number|boolean;
    y: number|boolean;
  }

  interface Sizes {
    width: number;
    height: number;
    realZoom: number;
    viewBox: {
      x: number;
      y: number;
      width: number;
      height: number;
    };
  }

  interface Instance {
    /**
     * Creates a new SvgPanZoom instance with given element selector.
     *
     * @param  {string|HTMLElement|SVGElement} svg selector of the tag on which it is to be applied.
     * @param  {Object} options        provides customization options at the initialization of the object.
     * @return {Instance}              Current instance
     */
    (svg: string|HTMLElement|SVGElement, options?: Options): Instance;

    /**
     * Enables Panning on svg element
     * @return {Instance} Current instance
     */
    enablePan(): Instance;

    /**
     * Disables panning on svg element
     * @return {Instance} Current instance
     */
    disablePan(): Instance;

    /**
     * Checks if Panning is enabled or not
     * @return {Boolean} true or false based on panning settings
     */
    isPanEnabled(): boolean;

    setBeforePan(fn: (oldPoint: Point, newPoint: Point) => void | boolean | PointModifier): Instance;

    setOnPan(fn: (point: Point) => void): Instance;

    /**
     * Pan to a rendered position
     *
     * @param  {Object} point {x: 0, y: 0}
     * @return {Instance}    Current instance
     */
    pan(point: Point): Instance;

    /**
     * Relatively pan the graph by a specified rendered position vector
     *
     * @param  {Object} point {x: 0, y: 0}
     * @return {Instance}    Current instance
     */
    panBy(point: Point): Instance;

    /**
     * Get pan vector
     *
     * @return {Object} {x: 0, y: 0}
     * @return {Instance}    Current instance
     */
    getPan(): Point;

    resetPan(): Instance;

    enableZoom(): Instance;

    disableZoom(): Instance;

    isZoomEnabled(): boolean;

    enableControlIcons(): Instance;

    disableControlIcons(): Instance;

    isControlIconsEnabled(): boolean;

    enableDblClickZoom(): Instance;

    disableDblClickZoom(): Instance;

    isDblClickZoomEnabled(): boolean;

    enableMouseWheelZoom(): Instance;

    disableMouseWheelZoom(): Instance;

    isMouseWheelZoomEnabled(): boolean;

    setZoomScaleSensitivity(scale: number): Instance;

    setMinZoom(zoom: number): Instance;

    setMaxZoom(zoom: number): Instance;

    setBeforeZoom(fn: (oldScale: number, newScale: number) => void | boolean): Instance;

    setOnZoom(fn: (scale: number) => void): Instance;

    zoom(scale: number): void;

    zoomIn(): Instance;

    zoomOut(): Instance;

    zoomBy(scale: number): Instance;

    zoomAtPoint(scale: number, point: Point): Instance;

    zoomAtPointBy(scale: number, point: Point): Instance;

    resetZoom(): Instance;

    /**
     * Get zoom scale/level
     *
     * @return {float} zoom scale
     */
    getZoom(): number;

    setOnUpdatedCTM(fn: (newCTM: SVGMatrix) => void): Instance;

    /**
     * Adjust viewport size (only) so it will fit in SVG
     * Does not center image
     *
     * @return {Instance}    Current instance
     */
    fit(): Instance;

    /**
     * Adjust viewport size (only) so it will contain in SVG
     * Does not center image
     *
     * @return {Instance}    Current instance
     */
    contain(): Instance;

    /**
     * Adjust viewport pan (only) so it will be centered in SVG
     * Does not zoom/fit image
     *
     * @return {Instance}    Current instance
     */
    center(): Instance;

    /**
     * Recalculates cached svg dimensions and controls position
     *
     * @return {Instance}    Current instance
     */
    resize(): Instance;

    /**
     * Get all calculate svg dimensions
     *
     * @return {Object} {width: 0, height: 0, realZoom: 0, viewBox: { width: 0, height: 0 }}
     */
    getSizes(): Sizes;

    reset(): Instance;

    /**
     * Update content cached BorderBox
     * Use when viewport contents change
     *
     * @return {Instance}    Current instance
     */
    updateBBox(): Instance;

    destroy(): void;
  }
}

declare const svgPanZoom: SvgPanZoom.Instance;

declare module "svg-pan-zoom" {
  export = svgPanZoom;
}
