var SvgUtils = require("./svg-utilities"),
  Utils = require("./utilities");

var ShadowViewport = function(viewport, options) {
  this.init(viewport, options);
};

/**
 * Initialization
 *
 * @param  {SVGElement} viewport
 * @param  {Object} options
 */
ShadowViewport.prototype.init = function(viewport, options) {
  // DOM Elements
  this.viewport = viewport;
  this.options = options;

  // State cache
  this.originalState = { zoom: 1, x: 0, y: 0 };
  this.activeState = { zoom: 1, x: 0, y: 0 };

  this.updateCTMCached = Utils.proxy(this.updateCTM, this);

  // Create a custom requestAnimationFrame taking in account refreshRate
  this.requestAnimationFrame = Utils.createRequestAnimationFrame(
    this.options.refreshRate
  );

  // ViewBox
  this.viewBox = { x: 0, y: 0, width: 0, height: 0 };
  this.cacheViewBox();

  // Process CTM
  var newCTM = this.processCTM();

  // Update viewport CTM and cache zoom and pan
  this.setCTM(newCTM);

  // Update CTM in this frame
  this.updateCTM();
};

/**
 * Cache initial viewBox value
 * If no viewBox is defined, then use viewport size/position instead for viewBox values
 */
ShadowViewport.prototype.cacheViewBox = function() {
  var svgViewBox = this.options.svg.getAttribute("viewBox");

  if (svgViewBox) {
    var viewBoxValues = svgViewBox
      .split(/[\s\,]/)
      .filter(function(v) {
        return v;
      })
      .map(parseFloat);

    // Cache viewbox x and y offset
    this.viewBox.x = viewBoxValues[0];
    this.viewBox.y = viewBoxValues[1];
    this.viewBox.width = viewBoxValues[2];
    this.viewBox.height = viewBoxValues[3];

    var zoom = Math.min(
      this.options.width / this.viewBox.width,
      this.options.height / this.viewBox.height
    );

    // Update active state
    this.activeState.zoom = zoom;
    this.activeState.x = (this.options.width - this.viewBox.width * zoom) / 2;
    this.activeState.y = (this.options.height - this.viewBox.height * zoom) / 2;

    // Force updating CTM
    this.updateCTMOnNextFrame();

    this.options.svg.removeAttribute("viewBox");
  } else {
    this.simpleViewBoxCache();
  }
};

/**
 * Recalculate viewport sizes and update viewBox cache
 */
ShadowViewport.prototype.simpleViewBoxCache = function() {
  var bBox = this.viewport.getBBox();

  this.viewBox.x = bBox.x;
  this.viewBox.y = bBox.y;
  this.viewBox.width = bBox.width;
  this.viewBox.height = bBox.height;
};

/**
 * Returns a viewbox object. Safe to alter
 *
 * @return {Object} viewbox object
 */
ShadowViewport.prototype.getViewBox = function() {
  return Utils.extend({}, this.viewBox);
};

/**
 * Get initial zoom and pan values. Save them into originalState
 * Parses viewBox attribute to alter initial sizes
 *
 * @return {CTM} CTM object based on options
 */
ShadowViewport.prototype.processCTM = function() {
  var newCTM = this.getCTM();

  if (this.options.fit || this.options.contain) {
    var newScale;
    if (this.options.fit) {
      newScale = Math.min(
        this.options.width / this.viewBox.width,
        this.options.height / this.viewBox.height
      );
    } else {
      newScale = Math.max(
        this.options.width / this.viewBox.width,
        this.options.height / this.viewBox.height
      );
    }

    newCTM.a = newScale; //x-scale
    newCTM.d = newScale; //y-scale
    newCTM.e = -this.viewBox.x * newScale; //x-transform
    newCTM.f = -this.viewBox.y * newScale; //y-transform
  }

  if (this.options.center) {
    var offsetX =
        (this.options.width -
          (this.viewBox.width + this.viewBox.x * 2) * newCTM.a) *
        0.5,
      offsetY =
        (this.options.height -
          (this.viewBox.height + this.viewBox.y * 2) * newCTM.a) *
        0.5;

    newCTM.e = offsetX;
    newCTM.f = offsetY;
  }

  // Cache initial values. Based on activeState and fix+center opitons
  this.originalState.zoom = newCTM.a;
  this.originalState.x = newCTM.e;
  this.originalState.y = newCTM.f;

  return newCTM;
};

/**
 * Return originalState object. Safe to alter
 *
 * @return {Object}
 */
ShadowViewport.prototype.getOriginalState = function() {
  return Utils.extend({}, this.originalState);
};

/**
 * Return actualState object. Safe to alter
 *
 * @return {Object}
 */
ShadowViewport.prototype.getState = function() {
  return Utils.extend({}, this.activeState);
};

/**
 * Get zoom scale
 *
 * @return {Float} zoom scale
 */
ShadowViewport.prototype.getZoom = function() {
  return this.activeState.zoom;
};

/**
 * Get zoom scale for pubilc usage
 *
 * @return {Float} zoom scale
 */
ShadowViewport.prototype.getRelativeZoom = function() {
  return this.activeState.zoom / this.originalState.zoom;
};

/**
 * Compute zoom scale for pubilc usage
 *
 * @return {Float} zoom scale
 */
ShadowViewport.prototype.computeRelativeZoom = function(scale) {
  return scale / this.originalState.zoom;
};

/**
 * Get pan
 *
 * @return {Object}
 */
ShadowViewport.prototype.getPan = function() {
  return { x: this.activeState.x, y: this.activeState.y };
};

/**
 * Return cached viewport CTM value that can be safely modified
 *
 * @return {SVGMatrix}
 */
ShadowViewport.prototype.getCTM = function() {
  var safeCTM = this.options.svg.createSVGMatrix();

  // Copy values manually as in FF they are not itterable
  safeCTM.a = this.activeState.zoom;
  safeCTM.b = 0;
  safeCTM.c = 0;
  safeCTM.d = this.activeState.zoom;
  safeCTM.e = this.activeState.x;
  safeCTM.f = this.activeState.y;

  return safeCTM;
};

/**
 * Set a new CTM
 *
 * @param {SVGMatrix} newCTM
 */
ShadowViewport.prototype.setCTM = function(newCTM) {
  var willZoom = this.isZoomDifferent(newCTM),
    willPan = this.isPanDifferent(newCTM);

  if (willZoom || willPan) {
    // Before zoom
    if (willZoom) {
      // If returns false then cancel zooming
      if (
        this.options.beforeZoom(
          this.getRelativeZoom(),
          this.computeRelativeZoom(newCTM.a)
        ) === false
      ) {
        newCTM.a = newCTM.d = this.activeState.zoom;
        willZoom = false;
      } else {
        this.updateCache(newCTM);
        this.options.onZoom(this.getRelativeZoom());
      }
    }

    // Before pan
    if (willPan) {
      var preventPan = this.options.beforePan(this.getPan(), {
          x: newCTM.e,
          y: newCTM.f
        }),
        // If prevent pan is an object
        preventPanX = false,
        preventPanY = false;

      // If prevent pan is Boolean false
      if (preventPan === false) {
        // Set x and y same as before
        newCTM.e = this.getPan().x;
        newCTM.f = this.getPan().y;

        preventPanX = preventPanY = true;
      } else if (Utils.isObject(preventPan)) {
        // Check for X axes attribute
        if (preventPan.x === false) {
          // Prevent panning on x axes
          newCTM.e = this.getPan().x;
          preventPanX = true;
        } else if (Utils.isNumber(preventPan.x)) {
          // Set a custom pan value
          newCTM.e = preventPan.x;
        }

        // Check for Y axes attribute
        if (preventPan.y === false) {
          // Prevent panning on x axes
          newCTM.f = this.getPan().y;
          preventPanY = true;
        } else if (Utils.isNumber(preventPan.y)) {
          // Set a custom pan value
          newCTM.f = preventPan.y;
        }
      }

      // Update willPan flag
      // Check if newCTM is still different
      if ((preventPanX && preventPanY) || !this.isPanDifferent(newCTM)) {
        willPan = false;
      } else {
        this.updateCache(newCTM);
        this.options.onPan(this.getPan());
      }
    }

    // Check again if should zoom or pan
    if (willZoom || willPan) {
      this.updateCTMOnNextFrame();
    }
  }
};

ShadowViewport.prototype.isZoomDifferent = function(newCTM) {
  return this.activeState.zoom !== newCTM.a;
};

ShadowViewport.prototype.isPanDifferent = function(newCTM) {
  return this.activeState.x !== newCTM.e || this.activeState.y !== newCTM.f;
};

/**
 * Update cached CTM and active state
 *
 * @param {SVGMatrix} newCTM
 */
ShadowViewport.prototype.updateCache = function(newCTM) {
  this.activeState.zoom = newCTM.a;
  this.activeState.x = newCTM.e;
  this.activeState.y = newCTM.f;
};

ShadowViewport.prototype.pendingUpdate = false;

/**
 * Place a request to update CTM on next Frame
 */
ShadowViewport.prototype.updateCTMOnNextFrame = function() {
  if (!this.pendingUpdate) {
    // Lock
    this.pendingUpdate = true;

    // Throttle next update
    this.requestAnimationFrame.call(window, this.updateCTMCached);
  }
};

/**
 * Update viewport CTM with cached CTM
 */
ShadowViewport.prototype.updateCTM = function() {
  var ctm = this.getCTM();

  // Updates SVG element
  SvgUtils.setCTM(this.viewport, ctm, this.defs);

  // Free the lock
  this.pendingUpdate = false;

  // Notify about the update
  if (this.options.onUpdatedCTM) {
    this.options.onUpdatedCTM(ctm);
  }
};

module.exports = function(viewport, options) {
  return new ShadowViewport(viewport, options);
};
