/*!
 * PathEditor 3.15.0
 * https://gsap.com
 *
 * Copyright 2008-2026, GreenSock. All rights reserved.
 * Subject to the terms at https://gsap.com/standard-license
 * @author: Jack Doyle, jack@greensock.com
*/
/* eslint-disable */

import { stringToRawPath, rawPathToString, bezierToPoints, simplifyPoints, pointsToSegment, subdivideSegment, getClosestData, copyRawPath, transformRawPath } from "./paths.js";
import { getGlobalMatrix, Matrix2D } from "./matrix.js";

let _numbersExp = /(?:(-)?\d*\.?\d*(?:e[\-+]?\d+)?)[0-9]/ig,
	_doc, _supportsPointer, _win, _body, gsap, _context,
	_selectionColor = "#4e7fff",
	_minimumMovement = 1,
	_DEG2RAD = Math.PI / 180,
	_getTime = Date.now || (() => new Date().getTime()),
	_lastInteraction = 0,
	_isPressed = 0,
	_emptyFunc = () => false,
	_interacted = () => _lastInteraction = _getTime(),
	_CTRL, _ALT, _SHIFT, _CMD,
	_recentlyAddedAnchor,
	_editingAxis = {}, //stores the x/y of the most recently-selected anchor point's x and y axis. We tap into this for snapping horizontally and vertically.
	_history = [],
	_point = {}, //reuse to minimize memory and maximize performance (mostly for snapping)
	_temp = [], //reuse this in places like getNormalizedSVG() to conserve memory
	_comma = ",",
	_selectedPaths = [],
	_preventDefault = event => {
		if (event.preventDefault) {
			event.preventDefault();
			if (event.preventManipulation) {
				event.preventManipulation();  //for some Microsoft browsers
			}
		}
	},
	_createElement = type => _doc.createElementNS ? _doc.createElementNS("http://www.w3.org/1999/xhtml", type) : _doc.createElement(type),
	_createSVG = (type, container, attributes) => {
		let element = _doc.createElementNS("http://www.w3.org/2000/svg", type),
			reg = /([a-z])([A-Z])/g,
			p;
		attributes = attributes || {};
		attributes.class = attributes.class || "path-editor";
		for (p in attributes) {
			if (element.style[p] !== undefined) {
				element.style[p] = attributes[p];
			} else {
				element.setAttributeNS(null, p.replace(reg, "$1-$2").toLowerCase(), attributes[p]);
			}
		}
		container.appendChild(element);
		return element;
	},
	_identityMatrixObject = {matrix:new Matrix2D()},
	_getConsolidatedMatrix = target => ((target.transform && target.transform.baseVal.consolidate()) || _identityMatrixObject).matrix,
	_getConcatenatedTransforms = target => {
		let m = _getConsolidatedMatrix(target),
			owner = target.ownerSVGElement;
		while ((target = target.parentNode) && target.ownerSVGElement === owner) {
			m.multiply(_getConsolidatedMatrix(target));
		}
		return "matrix(" + m.a + "," + m.b + "," + m.c + "," + m.d + "," + m.e + "," + m.f + ")";
	},
	_addHistory = pathEditor => {
		let selectedIndexes = [],
			a = pathEditor._selectedAnchors,
			i;
		for (i = 0; i < a.length; i++) {
			selectedIndexes[i] = a[i].i;
		}
		_history.unshift({path:pathEditor, d:pathEditor.path.getAttribute("d"), transform:pathEditor.path.getAttribute("transform") || "", selectedIndexes:selectedIndexes});
		if (_history.length > 30) {
			_history.length = 30;
		}
	},
	_round = value =>  ~~(value * 1000 + (value < 0 ? -.5 : .5)) / 1000,
	_getSquarePathData = size => {
		size = _round(size);
		return ["M-" + size, -size, size, -size, size, size, -size, size + "z"].join(_comma);
	},
	_getCirclePathData = size => {
		let circ = 0.552284749831,
			rcirc = _round(size * circ);
		size = _round(size);
		return "M" + size + ",0C" + [size, rcirc, rcirc, size, 0, size,  -rcirc, size, -size, rcirc, -size, 0, -size, -rcirc, -rcirc, -size, 0, -size, rcirc, -size, size, -rcirc, size, 0].join(_comma) + "z";
	},
	_checkDeselect = function(e) {
		if (!e.target._gsSelection && !_isPressed && _getTime() - _lastInteraction > 100) {
			let i = _selectedPaths.length;
			while (--i > -1) {
				_selectedPaths[i].deselect();
			}
			_selectedPaths.length = 0;
		}
	},
	_tempDiv, _touchEventLookup,
	_isMultiTouching = 0,
	_addListener = (element, type, func, capture) => {
		if (element.addEventListener) {
			let touchType = _touchEventLookup[type];
			capture = capture || {passive:false};
			element.addEventListener(touchType || type, func, capture);
			if (touchType && type !== touchType && touchType.substr(0, 7) !== "pointer") { //some browsers actually support both, so must we. But pointer events cover all.
				element.addEventListener(type, func, capture);
			}
		} else if (element.attachEvent) {
			element.attachEvent("on" + type, func);
		}
	},
	_removeListener = (element, type, func) => {
		if (element.removeEventListener) {
			let touchType = _touchEventLookup[type];
			element.removeEventListener(touchType || type, func);
			if (touchType && type !== touchType && touchType.substr(0, 7) !== "pointer") {
				element.removeEventListener(type, func);
			}
		} else if (element.detachEvent) {
			element.detachEvent("on" + type, func);
		}
	},
	_hasTouchID = (list, ID) => {
		let i = list.length;
		while (--i > -1) {
			if (list[i].identifier === ID) {
				return true;
			}
		}
		return false;
	},
	_onMultiTouchDocumentEnd = e => {
		_isMultiTouching = (e.touches && _dragCount < e.touches.length);
		_removeListener(e.target, "touchend", _onMultiTouchDocumentEnd);
	},
	_onMultiTouchDocument = e => {
		_isMultiTouching = (e.touches && _dragCount < e.touches.length);
		_addListener(e.target, "touchend", _onMultiTouchDocumentEnd);
	},
	_bind = (func, scope) => e => func.call(scope, e),
	_callback = (type, self, param) => {
		let callback = self.vars[type];
		if (callback) {
			callback.call(self.vars.callbackScope || self, param || self);
		}
		return self;
	},
	_copyElement,
	_resetSelection = () => {
		_copyElement.style.display = "block";
		_copyElement.select();
		_copyElement.style.display = "none";
	},
	_coreInitted,
	_initCore = (core) => {
		_doc = document;
		_win = window;
		_body = _doc.body;
		gsap = gsap || core || _win.gsap || console.warn("Please gsap.registerPlugin(PathEditor)");
		_context = (gsap && gsap.core.context) || function() {};
		_tempDiv = _createElement("div");
		_copyElement = _createElement("textarea");
		_copyElement.style.display = "none";
		_body && _body.appendChild(_copyElement);
		_touchEventLookup = (function(types) { //we create an object that makes it easy to translate touch event types into their "pointer" counterparts if we're in a browser that uses those instead. Like IE10 uses "MSPointerDown" instead of "touchstart", for example.
			let standard = types.split(","),
				converted = ((_tempDiv.onpointerdown !== undefined) ? "pointerdown,pointermove,pointerup,pointercancel" : (_tempDiv.onmspointerdown !== undefined) ? "MSPointerDown,MSPointerMove,MSPointerUp,MSPointerCancel" : types).split(","),
				obj = {},
				i = 4;
			while (--i > -1) {
				obj[standard[i]] = converted[i];
				obj[converted[i]] = standard[i];
			}
			return obj;
		}("touchstart,touchmove,touchend,touchcancel"));
		SVGElement.prototype.getTransformToElement = SVGElement.prototype.getTransformToElement || function(e) { //adds Chrome support
			return e.getScreenCTM().inverse().multiply(this.getScreenCTM());
		};
		_doc.addEventListener("keydown", function(e) {
			let key = e.keyCode || e.which,
				keyString = e.key || key,
				i, state, a, path;
			if (keyString === "Shift" || key === 16) {
				_SHIFT = true;
			} else if (keyString === "Control" || key === 17) {
				_CTRL = true;
			} else if (keyString === "Meta" || key === 91) {
				_CMD = true;
			} else if (keyString === "Alt" || key === 18) {
				_ALT = true;
				i = _selectedPaths.length;
				while (--i > -1) {
					_selectedPaths[i]._onPressAlt();
				}
			} else if ((keyString === "z" || key === 90) && (_CTRL || _CMD) && _history.length > 1) { //UNDO
				_history.shift();
				state = _history[0];
				if (state) {
					path = state.path;
					path.path.setAttribute("d", state.d);
					path.path.setAttribute("transform", state.transform);
					path.init();
					a = path._anchors;
					for (i = 0; i < a.length; i++) {
						if (state.selectedIndexes.indexOf(a[i].i) !== -1) {
							path._selectedAnchors.push(a[i]);
						}
					}
					path._updateAnchors();
					path.update();
					if (path.vars.onUndo) {
						path.vars.onUndo.call(path);
					}
				}
			} else if (keyString === "Delete" || keyString === "Backspace" || key === 8 || key === 46 || key === 63272 || (key === "d" && (_CTRL || _CMD))) { //DELETE
				i = _selectedPaths.length;
				while (--i > -1) {
					_selectedPaths[i]._deleteSelectedAnchors();
				}
			} else if ((keyString === "a" || key === 65) && (_CMD || _CTRL)) { //SELECT ALL
				i = _selectedPaths.length;
				while (--i > -1) {
					_selectedPaths[i].select(true);
				}
			}
		}, true);
		_doc.addEventListener("keyup", function(e) {
			let key = e.key || e.keyCode || e.which;
			if (key === "Shift" || key === 16) {
				_SHIFT = false;
			} else if (key === "Control" || key === 17) {
				_CTRL = false;
			} else if (key === "Meta" || key === 91) {
				_CMD = false;
			} else if (key === "Alt" || key === 18) {
				_ALT = false;
				let i = _selectedPaths.length;
				while (--i > -1) {
					_selectedPaths[i]._onReleaseAlt();
				}
			}
		}, true);
		_supportsPointer = !!_win.PointerEvent;
		_addListener(_doc, "mouseup", _checkDeselect);
		_addListener(_doc, "touchend", _checkDeselect);
		_addListener(_doc, "touchcancel", _emptyFunc); //some older Android devices intermittently stop dispatching "touchmove" events if we don't listen for "touchcancel" on the document. Very strange indeed.
		_addListener(_win, "touchmove", _emptyFunc); //works around Safari bugs that still allow the page to scroll even when we preventDefault() on the touchmove event.
		_body && _body.addEventListener("touchstart", _emptyFunc); //works around Safari bug: https://gsap.com/forums/topic/21450-draggable-in-iframe-on-mobile-is-buggy/
		_coreInitted = 1;
	},
	_onPress = function(e) {
		let self = this,
			ctm = getGlobalMatrix(self.target.parentNode, true), //previously used self.target.parentNode.getScreenCTM().inverse() but there's a major bug in Firefox that prevents it from working properly when there's an ancestor with a transform applied, so we bootstrapped our own solution that seems to work great across all browsers.
			touchEventTarget, temp;
		this._matrix = this.target.transform.baseVal.getItem(0).matrix;
		this._ctm = ctm;
		if (_touchEventLookup[e.type]) { //note: on iOS, BOTH touchmove and mousemove are dispatched, but the mousemove has pageY and pageX of 0 which would mess up the calculations and needlessly hurt performance.
			touchEventTarget = (e.type.indexOf("touch") !== -1) ? (e.currentTarget || e.target) : _doc; //pointer-based touches (for Microsoft browsers) don't remain locked to the original target like other browsers, so we must use the document instead. The event type would be "MSPointerDown" or "pointerdown".
			_addListener(touchEventTarget, "touchend", self._onRelease);
			_addListener(touchEventTarget, "touchmove", self._onMove);
			_addListener(touchEventTarget, "touchcancel", self._onRelease);
			_addListener(_doc, "touchstart", _onMultiTouchDocument);
			_addListener(_win, "touchforcechange", _preventDefault); //otherwise iOS will scroll when dragging.
		} else {
			touchEventTarget = null;
			_addListener(_doc, "mousemove", self._onMove); //attach these to the document instead of the box itself so that if the user's mouse moves too quickly (and off of the box), things still work.
		}
		if (!_supportsPointer) {
			_addListener(_doc, "mouseup", self._onRelease);
		}
		_preventDefault(e);
		_resetSelection(); // when a PathEditor is in an iframe in an environment like codepen, this helps avoid situations where the DELETE key won't actually work because the parent frame is intercepting the event.
		if (e.changedTouches) { //touch events store the data slightly differently
			e = self.touch = e.changedTouches[0];
			self.touchID = e.identifier;
		} else if (e.pointerId) {
			self.touchID = e.pointerId; //for some Microsoft browsers
		} else {
			self.touch = self.touchID = null;
		}
		self._startPointerY = self.pointerY = e.pageY; //record the starting x and y so that we can calculate the movement from the original in _onMouseMove
		self._startPointerX = self.pointerX = e.pageX;
		self._startElementX = self._matrix.e;
		self._startElementY = self._matrix.f;

		if (this._ctm.a === 1 && this._ctm.b === 0 && this._ctm.c === 0 && this._ctm.d === 1) {
			this._ctm = null;
		} else {
			temp = self._startPointerX * this._ctm.a + self._startPointerY * this._ctm.c + this._ctm.e;
			self._startPointerY = self._startPointerX * this._ctm.b + self._startPointerY * this._ctm.d + this._ctm.f;
			self._startPointerX = temp;
		}

		self.isPressed = _isPressed = true;
		self.touchEventTarget = touchEventTarget;
		if (self.vars.onPress) {
			self.vars.onPress.call(self.vars.callbackScope || self, self.pointerEvent);
		}
	},
	_onMove = function(e) {
		let self = this,
			originalEvent = e,
			touches, i;
		if (!self._enabled || _isMultiTouching || !self.isPressed || !e) {
			return;
		}
		self.pointerEvent = e;
		touches = e.changedTouches;
		if (touches) { //touch events store the data slightly differently
			e = touches[0];
			if (e !== self.touch && e.identifier !== self.touchID) { //Usually changedTouches[0] will be what we're looking for, but in case it's not, look through the rest of the array...(and Android browsers don't reuse the event like iOS)
				i = touches.length;
				while (--i > -1 && (e = touches[i]).identifier !== self.touchID) {}
				if (i < 0) {
					return;
				}
			}
		} else if (e.pointerId && self.touchID && e.pointerId !== self.touchID) { //for some Microsoft browsers, we must attach the listener to the doc rather than the trigger so that when the finger moves outside the bounds of the trigger, things still work. So if the event we're receiving has a pointerId that doesn't match the touchID, ignore it (for multi-touch)
			return;
		}
		_preventDefault(originalEvent);
		self.setPointerPosition(e.pageX, e.pageY);
		if (self.vars.onDrag) {
			self.vars.onDrag.call(self.vars.callbackScope || self, self.pointerEvent);
		}
	},
	_onRelease = function(e, force) {
		let self = this;
		if (!self._enabled || !self.isPressed || (e && self.touchID != null && !force && ((e.pointerId && e.pointerId !== self.touchID) || (e.changedTouches && !_hasTouchID(e.changedTouches, self.touchID))))) {  //for some Microsoft browsers, we must attach the listener to the doc rather than the trigger so that when the finger moves outside the bounds of the trigger, things still work. So if the event we're receiving has a pointerId that doesn't match the touchID, ignore it (for multi-touch)
			return;
		}
		_interacted();
		self.isPressed = _isPressed = false; //TODO: if we want to accommodate multi-touch, we'd need to introduce a counter to track how many touches there are and only toggle this when they're all off.
		let originalEvent = e,
			wasDragging = self.isDragging,
			touchEventTarget = self.touchEventTarget,
			touches, i;
		if (touchEventTarget) {
			_removeListener(touchEventTarget, "touchend", self._onRelease);
			_removeListener(touchEventTarget, "touchmove", self._onMove);
			_removeListener(touchEventTarget, "touchcancel", self._onRelease);
			_removeListener(_doc, "touchstart", _onMultiTouchDocument);
		} else {
			_removeListener(_doc, "mousemove", self._onMove);
		}
		if (!_supportsPointer) {
			_removeListener(_doc, "mouseup", self._onRelease);
			if (e && e.target) {
				_removeListener(e.target, "mouseup", self._onRelease);
			}
		}
		if (wasDragging) {
			self.isDragging = false;
		} else if (self.vars.onClick) {
			self.vars.onClick.call(self.vars.callbackScope || self, originalEvent);
		}
		if (e) {
			touches = e.changedTouches;
			if (touches) { //touch events store the data slightly differently
				e = touches[0];
				if (e !== self.touch && e.identifier !== self.touchID) { //Usually changedTouches[0] will be what we're looking for, but in case it's not, look through the rest of the array...(and Android browsers don't reuse the event like iOS)
					i = touches.length;
					while (--i > -1 && (e = touches[i]).identifier !== self.touchID) {}
					if (i < 0) {
						return;
					}
				}
			}
			self.pointerEvent = originalEvent;
			self.pointerX = e.pageX;
			self.pointerY = e.pageY;
		}
		if (originalEvent && !wasDragging && self.vars.onDragRelease) {
			self.vars.onDragRelease.call(self, self.pointerEvent);

		} else {
			if (originalEvent) {
				_preventDefault(originalEvent);
			}
			if (self.vars.onRelease) {
				self.vars.onRelease.call(self.vars.callbackScope || self, self.pointerEvent);
			}
		}
		if (wasDragging && self.vars.onDragEnd) {
			self.vars.onDragEnd.call(self.vars.callbackScope || self, self.pointerEvent);
		}
		return true;
	},
	_createSegmentAnchors = (rawPath, j, editor, vars) => {
		let segment = rawPath[j],
			l = segment.length - (segment.closed ? 6 : 0),
			a = [],
			i;
		for (i = 0; i < l; i+=6) {
			a.push(new Anchor(editor, rawPath, j, i, vars));
		}
		segment.closed && (a[0].isClosedStart = true);
		return a;
	},
	_getLength = (segment, i, i2) => { //i is the starting index, and it'll return the length to the next x/y pair. So if you're looking for the length to handle1, you'd feed in the index of the handle control point x whereas if you're looking for the length to handle2, i would be the x of the anchor.
		let x = segment[i2] - segment[i],
			y = segment[i2+1] - segment[i+1];
		return Math.sqrt(x * x + y * y);
	};


class DraggableSVG {

	constructor(target, vars) {
		this.target = (typeof(target) === "string") ? _doc.querySelectorAll(target)[0] : target;
		this.vars = vars || {};
		this._onPress = _bind(_onPress, this);
		this._onMove = _bind(_onMove, this);
		this._onRelease = _bind(_onRelease, this);
		this.target.setAttribute("transform", (this.target.getAttribute("transform") || "") + " translate(0,0)");
		this._matrix = _getConsolidatedMatrix(this.target);
		this.x = this._matrix.e;
		this.y = this._matrix.f;
		this.snap = vars.snap;
		if (!isNaN(vars.maxX) || !isNaN(vars.minX)) {
			this._bounds = 1;
			this.maxX = +vars.maxX;
			this.minX = +vars.minX;
		} else {
			this._bounds = 0;
		}
		this.enabled(true);
	}

	setPointerPosition(pointerX, pointerY) {
		let rnd = 1000,
			xChange, yChange, x, y, temp;
		this.pointerX = pointerX;
		this.pointerY = pointerY;
		if (this._ctm) {
			temp = pointerX * this._ctm.a + pointerY * this._ctm.c + this._ctm.e;
			pointerY = pointerX * this._ctm.b + pointerY * this._ctm.d + this._ctm.f;
			pointerX = temp;
		}
		yChange = (pointerY - this._startPointerY);
		xChange = (pointerX - this._startPointerX);
		if (yChange < _minimumMovement && yChange > -_minimumMovement) {
			yChange = 0;
		}
		if (xChange < _minimumMovement && xChange > -_minimumMovement) {
			xChange = 0;
		}
		x = (((this._startElementX + xChange) * rnd) | 0) / rnd;
		y = (((this._startElementY + yChange) * rnd) | 0) / rnd;
		if (this.snap && !_SHIFT) {
			_point.x = x;
			_point.y = y;
			this.snap.call(this, _point);
			x = _point.x;
			y = _point.y;
		}
		if (this.x !== x || this.y !== y) {
			this._matrix.f = this.y = y;
			this._matrix.e = this.x = x;
			if (!this.isDragging && this.isPressed) {
				this.isDragging = true;
				_callback("onDragStart", this, this.pointerEvent);
			}
		}
	}

	enabled(enabled) {
		if (!arguments.length) {
			return this._enabled;
		}
		let dragging;
		this._enabled = enabled;
		if (enabled) {
			if (!_supportsPointer) {
				_addListener(this.target, "mousedown", this._onPress);
			}
			_addListener(this.target, "touchstart", this._onPress);
			_addListener(this.target, "click", this._onClick, true); //note: used to pass true for capture but it prevented click-to-play-video functionality in Firefox.
		} else {
			dragging = this.isDragging;
			_removeListener(this.target, "mousedown", this._onPress);
			_removeListener(this.target, "touchstart", this._onPress);
			_removeListener(_win, "touchforcechange", _preventDefault);
			_removeListener(this.target, "click", this._onClick);
			if (this.touchEventTarget) {
				_removeListener(this.touchEventTarget, "touchcancel", this._onRelease);
				_removeListener(this.touchEventTarget, "touchend", this._onRelease);
				_removeListener(this.touchEventTarget, "touchmove", this._onMove);
			}
			_removeListener(_doc, "mouseup", this._onRelease);
			_removeListener(_doc, "mousemove", this._onMove);
			this.isDragging = this.isPressed = false;
			if (dragging) {
				_callback("onDragEnd", this, this.pointerEvent);
			}
		}
		return this;
	}

	endDrag(e) {
		this._onRelease(e);
	}

}





class Anchor {

	constructor(editor, rawPath, j, i, vars) {
		this.editor = editor;
		this.element = _createSVG("path", editor._selection, {fill:_selectionColor, stroke:_selectionColor, strokeWidth:2, vectorEffect:"non-scaling-stroke"});
		this.update(rawPath, j, i);
		this.element._gsSelection = true;
		this.vars = vars || {};
		this._draggable = new DraggableSVG(this.element, {callbackScope:this, onDrag:this.onDrag, snap:this.vars.snap, onPress:this.onPress, onRelease:this.onRelease, onClick:this.onClick, onDragEnd:this.onDragEnd});
	}

	onPress() {
		_callback("onPress", this);
	}

	onClick() {
		_callback("onClick", this);
	}

	onDrag() {
		let s = this.segment;
		this.vars.onDrag.call(this.vars.callbackScope || this, this, this._draggable.x - s[this.i], this._draggable.y - s[this.i+1]);
	}

	onDragEnd() {
		_callback("onDragEnd", this);
	}

	onRelease() {
		_callback("onRelease", this);
	}

	update(rawPath, j, i) {
		if (rawPath) {
			this.rawPath = rawPath;
		}
		if (arguments.length <= 1) {
			j = this.j;
			i = this.i;
		} else {
			this.j = j;
			this.i = i;
		}
		let prevSmooth = this.smooth,
			segment = this.rawPath[j],
			pi = i === 0 && segment.closed ? segment.length - 4 : i - 2;
		this.segment = segment;
		this.smooth = (i > 0 && i < segment.length - 2 && Math.abs(Math.atan2(segment[i+1] - segment[pi+1], segment[i] - segment[pi]) - Math.atan2(segment[i+3] - segment[i+1], segment[i+2] - segment[i])) < 0.09) ? 2 : 0; //0: corner, 1: smooth but not mirrored, 2: smooth and mirrored.
		if (this.smooth !== prevSmooth) {
			this.element.setAttribute("d", this.smooth ? this.editor._circleHandle : this.editor._squareHandle);
		}
		this.element.setAttribute("transform", "translate(" + segment[i] + "," + segment[i+1] + ")");
	}
}




export class PathEditor {

	constructor(target, vars) {
		vars = vars || {};
		_coreInitted || _initCore();
		this.vars = vars;
		this.path = (typeof(target) === "string") ? _doc.querySelectorAll(target)[0] : target;
		this._g = _createSVG("g", this.path.ownerSVGElement, {class:"path-editor-g path-editor"});
		this._selectionHittest = _createSVG("path", this._g, {stroke:"transparent", strokeWidth:16, fill:"none", vectorEffect:"non-scaling-stroke"});
		this._selection = vars._selection || _createSVG("g", this._g, {class:"path-editor-selection path-editor"});
		this._selectionPath = _createSVG("path", this._selection, {stroke:_selectionColor, strokeWidth:2, fill:"none", vectorEffect:"non-scaling-stroke"});
		this._selectedAnchors = [];
		this._line1 = _createSVG("polyline", this._selection, {stroke:_selectionColor, strokeWidth:2, vectorEffect:"non-scaling-stroke"});
		this._line2 = _createSVG("polyline", this._selection, {stroke:_selectionColor, strokeWidth:2, vectorEffect:"non-scaling-stroke"});
		this._line1.style.pointerEvents = this._line2.style.pointerEvents = this._selectionPath.style.pointerEvents = "none";
		this._enabled = true;
		let ctm = this.path.parentNode.getScreenCTM().inverse(),
			size = (ctm.a + ctm.d) / 2 * (vars.handleSize || 5);
		this._squareHandle = _getSquarePathData(size);
		this._circleHandle = _getCirclePathData(size * 1.15);
		this._handle1 = _createSVG("path", this._selection, {d:this._squareHandle, fill:_selectionColor, stroke:"transparent", strokeWidth:6});
		this._handle2 = _createSVG("path", this._selection, {d:this._squareHandle, fill:_selectionColor, stroke:"transparent", strokeWidth:6});
		this._handle1._draggable = new DraggableSVG(this._handle1, {onDrag:this._onDragHandle1, callbackScope:this, onPress:this._onPressHandle1, onRelease:this._onReleaseHandle, onClick:this._onClickHandle1, snap:vars.handleSnap});
		this._handle2._draggable = new DraggableSVG(this._handle2, {onDrag:this._onDragHandle2, callbackScope:this, onPress:this._onPressHandle2, onRelease:this._onReleaseHandle, onClick:this._onClickHandle2, snap:vars.handleSnap});
		this._handle1.style.visibility = this._handle2.style.visibility = "hidden";
		let selectionItems = [this._handle1, this._handle2, this._line1, this._line2, this._selection, this._selectionPath, this._selectionHittest],
			i = selectionItems.length;
		while (--i > -1) {
			selectionItems[i]._gsSelection = true; //just a flag we can check in the _checkDeselect() method to detect clicks on things that are selection-related.
		}
		if (vars.draggable !== false) {
			this._draggable = new DraggableSVG(this._selectionHittest, {callbackScope:this, onPress:this.select, onRelease:this._onRelease, onDrag:this._onDragPath, onDragEnd:this._saveState, maxX:this.vars.maxX, minX:this.vars.minX});
		}
		this.init();
		this._selection.style.visibility = (vars.selected === false) ? "hidden" : "visible";
		if (vars.selected !== false) {
			this.path._gsSelection = true;
			_selectedPaths.push(this);
		}
		this._saveState();
		if (!_supportsPointer) {
			_addListener(this._selectionHittest, "mousedown", _bind(this._onClickSelectionPath, this));
			_addListener(this._selectionHittest, "mouseup", _bind(this._onRelease, this));
		}
		_addListener(this._selectionHittest, "touchstart", _bind(this._onClickSelectionPath, this));
		_addListener(this._selectionHittest, "touchend", _bind(this._onRelease, this));
		_context(this);
	}

	_onRelease(e) {
		let anchor = this._editingAnchor;
		if (anchor) {
			_editingAxis.x = anchor.segment[anchor.i];
			_editingAxis.y = anchor.segment[anchor.i+1];
		}
		_removeListener(_win, "touchforcechange", _preventDefault); //otherwise iOS will scroll when dragging.
		_callback("onRelease", this, e);
	}

	init() {
		let pathData = this.path.getAttribute("d"),
			rawPath = stringToRawPath(pathData),
			transform = this.path.getAttribute("transform") || "translate(0,0)",
			createAnchors = (!this._rawPath || rawPath.totalPoints !== this._rawPath.totalPoints || rawPath.map(s => s.length).join(",") !== this._rawPath.map(s => s.length).join(",")),
			anchorVars = {callbackScope:this, snap:this.vars.anchorSnap, onDrag:this._onDragAnchor, onPress:this._onPressAnchor, onRelease:this._onRelease, onClick:this._onClickAnchor, onDragEnd:this._onDragEndAnchor, maxX:this.vars.maxX, minX:this.vars.minX},
			l, i;

		if (createAnchors && this._anchors && this._anchors.length) {
			for (i = 0; i < this._anchors.length; i++) {
				this._anchors[i].element.parentNode.removeChild(this._anchors[i].element);
				this._anchors[i]._draggable.enabled(false);
			}
			this._selectedAnchors.length = 0;
		}
		this._rawPath = rawPath;
		if (createAnchors) {
			this._anchors = _createSegmentAnchors(rawPath, 0, this, anchorVars);
			l = rawPath.length;
			if (l > 1) {
				for (i = 1; i < l; i++) {
					this._anchors = this._anchors.concat(_createSegmentAnchors(rawPath, i, this, anchorVars));
				}
			}
		} else {
			i = this._anchors.length;
			while (--i > -1) {
				this._anchors[i].update(rawPath);
			}
		}

		this._selection.appendChild(this._handle1); //for stacking order (handles should always be on top)
		this._selection.appendChild(this._handle2);
		//		this._selectedAnchors.length = 0;
		this._selectionPath.setAttribute("d", pathData);
		this._selectionHittest.setAttribute("d", pathData);
		this._g.setAttribute("transform", _getConcatenatedTransforms(this.path.parentNode) || "translate(0,0)");
		this._selection.setAttribute("transform", transform);
		this._selectionHittest.setAttribute("transform", transform);
		this._updateAnchors();
		return this;
	}

	_saveState() {
		_addHistory(this);
	}

	_onClickSelectionPath(e) {
		if (this._selection.style.visibility === "hidden") {
			this.select();
		} else if (_ALT || (e && e.altKey)) {
			let anchorVars = {callbackScope:this, snap:this.vars.anchorSnap, onDrag:this._onDragAnchor, onPress:this._onPressAnchor, onRelease:this._onRelease, onClick:this._onClickAnchor, onDragEnd:this._onDragEndAnchor, maxX:this.vars.maxX, minX:this.vars.minX},
				ctm = this._selection.getScreenCTM().inverse(),
				newIndex, i, anchor, x, y, closestData;
			if (this._draggable) {
				this._draggable._onRelease(e); //otherwise, ALT-click/dragging on a path would create a new anchor AND drag the entire path.
			}
			if (ctm) {
				x = e.clientX * ctm.a + e.clientY * ctm.c + ctm.e;
				y = e.clientX * ctm.b + e.clientY * ctm.d + ctm.f;
			}
			//DEBUG: _createSVG("circle", this._selection, {fill:"red", r:5, cx:x, cy:y});
			closestData = getClosestData(this._rawPath, x, y);
			subdivideSegment(this._rawPath[closestData.j], closestData.i, closestData.t);
			newIndex = closestData.i + 6;
			for (i = 0; i < this._anchors.length; i++) {
				if (this._anchors[i].i >= newIndex && this._anchors[i].j === closestData.j) {
					this._anchors[i].i += 6;
				}
			}
			anchor = new Anchor(this, this._rawPath, closestData.j, newIndex, anchorVars);
			this._selection.appendChild(this._handle1); //for stacking order (handles should always be on top)
			this._selection.appendChild(this._handle2);
			anchor._draggable._onPress(e);
			_recentlyAddedAnchor = anchor;
			this._anchors.push(anchor);
			this._selectedAnchors.length = 0;
			this._selectedAnchors.push(anchor);
			this._updateAnchors();
			this.update();
			this._saveState();
		}
		_resetSelection();
		_addListener(_win, "touchforcechange", _preventDefault); //otherwise iOS will scroll when dragging.
		_callback("onPress", this);
	}

	_onClickHandle1() {
		let anchor = this._editingAnchor,
			i = anchor.i,
			s = anchor.segment,
			pi = anchor.isClosedStart ? s.length - 4 : i - 2;
		if (_ALT && Math.abs(s[i] - s[pi]) < 5 && Math.abs(s[i+1] - s[pi+1]) < 5) {
			this._onClickAnchor(anchor);
		}
	}

	_onClickHandle2() {
		let anchor = this._editingAnchor,
			i = anchor.i,
			s = anchor.segment;
		if (_ALT && Math.abs(s[i] - s[i+2]) < 5 && Math.abs(s[i+1] - s[i+3]) < 5) {
			this._onClickAnchor(anchor);
		}
	}

	_onDragEndAnchor(e) {
		_recentlyAddedAnchor = null;
		this._saveState();
	}

	isSelected() {
		return (this._selectedAnchors.length > 0 || this._selection.style.visibility === "visible");
	}

	select(allAnchors) {
		this._selection.style.visibility = "visible";
		this._editingAnchor = null;
		this.path._gsSelection = true;
		if (allAnchors === true) {
			let i = this._anchors.length;
			while (--i > -1) {
				this._selectedAnchors[i] = this._anchors[i];
			}
		}
		if (_selectedPaths.indexOf(this) === -1) {
			_selectedPaths.push(this);
		}
		this._updateAnchors();
		return this;
	}

	deselect() {
		this._selection.style.visibility = "hidden";
		this._selectedAnchors.length = 0;
		this._editingAnchor = null;
		this.path._gsSelection = false;
		_selectedPaths.splice(_selectedPaths.indexOf(this), 1);
		this._updateAnchors();
		return this;
	}

	_onDragPath(e) {
		let transform = this._selectionHittest.getAttribute("transform") || "translate(0,0)";
		this._selection.setAttribute("transform", transform);
		this.path.setAttribute("transform", transform);
	}

	_onPressAnchor(anchor) {
		if (this._selectedAnchors.indexOf(anchor) === -1) { //if it isn't already selected...
			if (!_SHIFT) {
				this._selectedAnchors.length = 0;
			}
			this._selectedAnchors.push(anchor);
		} else if (_SHIFT) {
			this._selectedAnchors.splice(this._selectedAnchors.indexOf(anchor), 1);
			anchor._draggable.endDrag();
		}
		_editingAxis.x = anchor.segment[anchor.i];
		_editingAxis.y = anchor.segment[anchor.i+1];
		this._updateAnchors();
		_callback("onPress", this);
	}

	_deleteSelectedAnchors() {
		let anchors = this._selectedAnchors,
			i = anchors.length,
			anchor, index, j, jIndex;
		while (--i > -1) {
			anchor = anchors[i];
			anchor.element.parentNode.removeChild(anchor.element);
			anchor._draggable.enabled(false);
			index = anchor.i;
			jIndex = anchor.j;
			if (!index) { //first
				anchor.segment.splice(index, 6);
			} else if (index < anchor.segment.length - 2) {
				anchor.segment.splice(index-2, 6);
			} else { //last
				anchor.segment.splice(index-4, 6);
			}
			anchors.splice(i, 1);
			this._anchors.splice(this._anchors.indexOf(anchor), 1);
			for (j = 0; j < this._anchors.length; j++) {
				if (this._anchors[j].i >= index && this._anchors[j].j === jIndex) {
					this._anchors[j].i -= 6;
				}
			}
		}
		this._updateAnchors();
		this.update();
		this._saveState();
		if (this.vars.onDeleteAnchor) {
			this.vars.onDeleteAnchor.call(this.vars.callbackScope || this);
		}
	}

	_onClickAnchor(anchor) {
		let i = anchor.i,
			segment = anchor.segment,
			pi = anchor.isClosedStart ? segment.length - 4 : i - 2,
			rnd = 1000,
			isEnd = (!i || i >= segment.length - 2),
			angle1, angle2, length1, length2, sin, cos;
		if (_ALT && _recentlyAddedAnchor !== anchor && this._editingAnchor) {
			anchor.smooth = !anchor.smooth;
			if (isEnd && !anchor.isClosedStart) { //the very ends can't be "smooth"
				anchor.smooth = false;
			}
			anchor.element.setAttribute("d", anchor.smooth ? this._circleHandle : this._squareHandle);
			if (anchor.smooth && (!isEnd || anchor.isClosedStart)) {
				angle1 = Math.atan2(segment[i+1] - segment[pi+1], segment[i] - segment[pi]);
				angle2 = Math.atan2(segment[i+3] - segment[i+1], segment[i+2] - segment[i]);
				angle1 = (angle1 + angle2) / 2;
				length1 = _getLength(segment, pi, i);
				length2 = _getLength(segment, i, i+2);
				if (length1 < 0.2) {
					length1 = (_getLength(segment, i, pi-4) / 4);
					angle1 = angle2 || Math.atan2(segment[i+7] - segment[pi-3], segment[i+6] - segment[pi-4]);
				}
				if (length2 < 0.2) {
					length2 = (_getLength(segment, i, i+6) / 4);
					angle2 = angle1 || Math.atan2(segment[i+7] - segment[pi-3], segment[i+6] - segment[pi-4]);
				}
				sin = Math.sin(angle1);
				cos = Math.cos(angle1);
				if (Math.abs(angle2 - angle1) < Math.PI / 2) {
					sin = -sin;
					cos = -cos;
				}
				segment[pi] = (((segment[i] + cos * length1) * rnd) | 0) / rnd;
				segment[pi+1] = (((segment[i+1] + sin * length1) * rnd) | 0) / rnd;
				segment[i+2] = (((segment[i] - cos * length2) * rnd) | 0) / rnd;
				segment[i+3] = (((segment[i+1] - sin * length2) * rnd) | 0) / rnd;
				this._updateAnchors();
				this.update();
				this._saveState();
			} else if (!anchor.smooth && (!isEnd || anchor.isClosedStart)) {
				if (i || anchor.isClosedStart) {
					segment[pi] = segment[i];
					segment[pi+1] = segment[i+1];
				}
				if (i < segment.length - 2) {
					segment[i+2] = segment[i];
					segment[i+3] = segment[i+1];
				}
				this._updateAnchors();
				this.update();
				this._saveState();
			}
		} else if (!_SHIFT) {
			this._selectedAnchors.length = 0;
			this._selectedAnchors.push(anchor);
		}
		_recentlyAddedAnchor = null;
		this._updateAnchors();
	}

	_updateAnchors() {
		let anchor = (this._selectedAnchors.length === 1) ? this._selectedAnchors[0] : null,
			segment = anchor ? anchor.segment : null,
			i, x, y;
		this._editingAnchor = anchor;
		for (i = 0; i < this._anchors.length; i++) {
			this._anchors[i].element.style.fill = (this._selectedAnchors.indexOf(this._anchors[i]) !== -1) ? _selectionColor : "white";
			//this._anchors[i].element.setAttribute("fill", (this._selectedAnchors.indexOf(this._anchors[i]) !== -1) ? _selectionColor : "white");
		}
		if (anchor) {
			this._handle1.setAttribute("d", anchor.smooth ? this._circleHandle : this._squareHandle);
			this._handle2.setAttribute("d", anchor.smooth ? this._circleHandle : this._squareHandle);
		}
		i = anchor ? anchor.i : 0;
		if (anchor && (i || anchor.isClosedStart)) {
			x = anchor.isClosedStart ? segment[segment.length-4] : segment[i-2];
			y = anchor.isClosedStart ? segment[segment.length-3] : segment[i-1]; //TODO: if they equal the anchor coordinates, just hide it.
			this._handle1.style.visibility = this._line1.style.visibility = (!_ALT && x === segment[i] && y === segment[i+1]) ? "hidden" : "visible";
			this._handle1.setAttribute("transform", "translate(" + x + _comma + y + ")");
			this._line1.setAttribute("points",  x + _comma + y + _comma + segment[i] + _comma + segment[i+1]);
		} else {
			this._handle1.style.visibility = this._line1.style.visibility = "hidden";
		}
		if (anchor && i < segment.length - 2) {
			x = segment[i+2];
			y = segment[i+3];
			this._handle2.style.visibility = this._line2.style.visibility = (!_ALT && x === segment[i] && y === segment[i+1]) ? "hidden" : "visible";
			this._handle2.setAttribute("transform", "translate(" + x + _comma + y + ")");
			this._line2.setAttribute("points",  segment[i] + _comma + segment[i+1] + _comma + x + _comma + y);

		} else {
			this._handle2.style.visibility = this._line2.style.visibility = "hidden";
		}
	}

	_onPressAlt() {
		let anchor = this._editingAnchor;
		if (anchor) {
			if (anchor.i || anchor.isClosedStart) {
				this._handle1.style.visibility = this._line1.style.visibility = "visible";
			}
			if (anchor.i < anchor.segment.length - 2) {
				this._handle2.style.visibility = this._line2.style.visibility = "visible";
			}
		}
	}

	_onReleaseAlt() {
		let anchor = this._editingAnchor,
			s, i, pi;
		if (anchor) {
			s = anchor.segment;
			i = anchor.i;
			pi = anchor.isClosedStart ? s.length - 4 : i - 2;
			if (s[i] === s[pi] && s[i+1] === s[pi+1]) {
				this._handle1.style.visibility = this._line1.style.visibility = "hidden";
			}
			if (s[i] === s[i+2] && s[i+1] === s[i+3]) {
				this._handle2.style.visibility = this._line2.style.visibility = "hidden";
			}
		}
	}

	_onPressHandle1() {
		if (this._editingAnchor.smooth) {
			this._oppositeHandleLength = _getLength(this._editingAnchor.segment, this._editingAnchor.i, this._editingAnchor.i+2);
		}
		_callback("onPress", this);
	}

	_onPressHandle2() {
		if (this._editingAnchor.smooth) {
			this._oppositeHandleLength = _getLength(this._editingAnchor.segment, this._editingAnchor.isClosedStart ? this._editingAnchor.segment.length - 4 : this._editingAnchor.i-2, this._editingAnchor.i);
		}
		_callback("onPress", this);
	}

	_onReleaseHandle(e) {
		this._onRelease(e);
		this._saveState();
	}

	_onDragHandle1() {
		let anchor = this._editingAnchor,
			s = anchor.segment,
			i = anchor.i,
			pi = anchor.isClosedStart ? s.length-4 : i-2,
			rnd = 1000,
			x = this._handle1._draggable.x,
			y = this._handle1._draggable.y,
			angle;
		s[pi] = x = ((x * rnd) | 0) / rnd;
		s[pi+1] = y = ((y * rnd) | 0) / rnd;
		if (anchor.smooth) {
			if (_ALT) {
				anchor.smooth = false;
				anchor.element.setAttribute("d", this._squareHandle);
				this._handle1.setAttribute("d", this._squareHandle);
				this._handle2.setAttribute("d", this._squareHandle);
			} else {
				angle = Math.atan2(s[i+1] - y, s[i] - x);
				x = this._oppositeHandleLength * Math.cos(angle);
				y = this._oppositeHandleLength * Math.sin(angle);
				s[i+2] = (((s[i] + x) * rnd) | 0) / rnd;
				s[i+3] = (((s[i+1] + y) * rnd) | 0) / rnd;
			}

		}
		this.update();
	}

	_onDragHandle2() {
		let anchor = this._editingAnchor,
			s = anchor.segment,
			i = anchor.i,
			pi = anchor.isClosedStart ? s.length-4 : i-2,
			rnd = 1000,
			x = this._handle2._draggable.x,
			y = this._handle2._draggable.y,
			angle;
		s[i+2] = x = ((x * rnd) | 0) / rnd;
		s[i+3] = y = ((y * rnd) | 0) / rnd;
		if (anchor.smooth) {
			if (_ALT) {
				anchor.smooth = false;
				anchor.element.setAttribute("d", this._squareHandle);
				this._handle1.setAttribute("d", this._squareHandle);
				this._handle2.setAttribute("d", this._squareHandle);
			} else {
				angle = Math.atan2(s[i+1] - y, s[i] - x);
				x = this._oppositeHandleLength * Math.cos(angle);
				y = this._oppositeHandleLength * Math.sin(angle);
				s[pi] = (((s[i] + x) * rnd) | 0) / rnd;
				s[pi+1] = (((s[i+1] + y) * rnd) | 0) / rnd;
			}

		}
		this.update();
	}

	_onDragAnchor(anchor, changeX, changeY) {
		let anchors = this._selectedAnchors,
			l = anchors.length,
			rnd = 1000,
			i, j, s, a, pi;
		for (j = 0; j < l; j++) {
			a = anchors[j];
			i = a.i;
			s = a.segment;
			if (i) {
				s[i-2] = (((s[i-2] + changeX) * rnd) | 0) / rnd;
				s[i-1] = (((s[i-1] + changeY) * rnd) | 0) / rnd;
			} else if (a.isClosedStart) {
				pi = s.length-2;
				s[pi] = _round(s[pi] + changeX);
				s[pi+1] = _round(s[pi+1] + changeY);
				s[pi-2] = _round(s[pi-2] + changeX);
				s[pi-1] = _round(s[pi-1] + changeY);
			}
			s[i] = (((s[i] + changeX) * rnd) | 0) / rnd;
			s[i+1] = (((s[i+1] + changeY) * rnd) | 0) / rnd;
			if (i < s.length - 2) {
				s[i+2] = (((s[i+2] + changeX) * rnd) | 0) / rnd;
				s[i+3] = (((s[i+3] + changeY) * rnd) | 0) / rnd;
			}
			if (a !== anchor) {
				a.element.setAttribute("transform", "translate(" + (s[i]) + _comma + (s[i+1]) + ")");
			}
		}
		this.update();
	}

	enabled(enabled) {
		if (!arguments.length) {
			return this._enabled;
		}
		let i = this._anchors.length;
		while (--i > -1) {
			this._anchors[i]._draggable.enabled(enabled);
		}
		this._enabled = enabled;
		this._handle1._draggable.enabled(enabled);
		this._handle2._draggable.enabled(enabled);
		if (this._draggable) {
			this._draggable.enabled(enabled);
		}
		if (!enabled) {
			this.deselect();
			this._selectionHittest.parentNode && this._selectionHittest.parentNode.removeChild(this._selectionHittest);
			this._selection.parentNode && this._selection.parentNode.removeChild(this._selection);
		} else if (!this._selection.parentNode) {
			this.path.ownerSVGElement.appendChild(this._selectionHittest);
			this.path.ownerSVGElement.appendChild(this._selection);
			this.init();
			this._saveState();
		}
		this._updateAnchors();
		return this.update();
	}

	update(readPath) {
		let d = "",
			anchor = this._editingAnchor,
			i, s, x, y, pi;
		if (readPath) {
			this.init();
		}
		if (anchor) {
			i = anchor.i;
			s = anchor.segment;
			if (i || anchor.isClosedStart) {
				pi = anchor.isClosedStart ? s.length-4 : i-2;
				x = s[pi];
				y = s[pi+1];
				this._handle1.setAttribute("transform", "translate(" + x + _comma + y + ")");
				this._line1.setAttribute("points", x + _comma + y + _comma + s[i] + _comma + s[i+1]);
			}
			if (i < s.length - 2) {
				x = s[i+2];
				y = s[i+3];
				this._handle2.setAttribute("transform", "translate(" + (x) + _comma + (y) + ")");
				this._line2.setAttribute("points", s[i] + _comma + s[i+1] + _comma + x + _comma + y);
			}
		}

		if (readPath) {
			d = this.path.getAttribute("d");
		} else {
			for (i = 0; i < this._rawPath.length; i++) {
				s = this._rawPath[i];
				if (s.length > 7) {
					d += "M" + s[0] + _comma + s[1] + "C" + s.slice(2).join(_comma);
				}
			}
			this.path.setAttribute("d", d);
			this._selectionPath.setAttribute("d", d);
			this._selectionHittest.setAttribute("d", d);
		}

		if (this.vars.onUpdate && this._enabled) {
			_callback("onUpdate", this, d);
		}
		return this;
	}

	getRawPath(applyTransforms, offsetX, offsetY) {
		if (applyTransforms) {
			let m = _getConsolidatedMatrix(this.path);
			return transformRawPath(copyRawPath(this._rawPath), 1, 0, 0, 1, m.e + (offsetX || 0), m.f + (offsetY || 0));
		}
		return this._rawPath;
	}

	getString(applyTransforms, offsetX, offsetY) {
		if (applyTransforms) {
			let m = _getConsolidatedMatrix(this.path);
			return rawPathToString(transformRawPath(copyRawPath(this._rawPath), 1, 0, 0, 1, m.e + (offsetX || 0), m.f + (offsetY || 0)));
		}
		return this.path.getAttribute("d");
	}

	getNormalizedSVG(height, originY, shorten, onEaseError) {
		let s = this._rawPath[0],
			tx = s[0] * -1,
			ty = (originY === 0) ? 0 : -(originY || s[1]),
			l = s.length,
			sx = 1 / (s[l-2] + tx),
			sy = -height || (s[l-1] + ty),
			rnd = 1000,
			points, i, x1, y1, x2, y2;
		_temp.length = 0;
		if (sy) { //typically y ends at 1 (so that the end values are reached)
			sy = 1 / sy;
		} else { //in case the ease returns to its beginning value, scale everything proportionally
			sy = -sx;
		}
		sx *= rnd;
		sy *= rnd;
		for (i = 0; i < l; i += 2) {
			_temp[i] = (((s[i] + tx) * sx) | 0) / rnd;
			_temp[i+1] = (((s[i+1] + ty) * sy) | 0) / rnd;
		}

		if (onEaseError) {
			points = [];
			l = _temp.length;
			for (i = 2; i < l; i+=6) {
				x1 = _temp[i-2];
				y1 = _temp[i-1];
				x2 = _temp[i+4];
				y2 = _temp[i+5];
				points.push(x1, y1, x2, y2);
				bezierToPoints(x1, y1, _temp[i], _temp[i+1], _temp[i+2], _temp[i+3], x2, y2, 0.001, points, points.length - 2);
			}
			x1 = points[0];
			l = points.length;
			for (i = 2; i < l; i+=2) {
				x2 = points[i];
				if (x2 < x1 || x2 > 1 || x2 < 0) {
					onEaseError();
					break;
				}
				x1 = x2;
			}
		}

		if (shorten && l === 8 && _temp[0] === 0 && _temp[1] === 0 && _temp[l-2] === 1 && _temp[l-1] === 1) {
			return _temp.slice(2, 6).join(",");
		}
		_temp[2] = "C" + _temp[2];
		return "M" + _temp.join(",");
	}

	kill() {
		this.enabled(false);
		this._g.parentNode && this._g.parentNode.removeChild(this._g);
	}

	revert() {
		this.kill();
	}

}





PathEditor.simplifyPoints = simplifyPoints;
PathEditor.pointsToSegment = pointsToSegment;
PathEditor.simplifySVG = (data, vars) => {  //takes a <path> element or data string and simplifies it according to whatever tolerance you set (default:1, the bigger the number the more variance there can be). vars: {tolerance:1, cornerThreshold:degrees, curved:true}
	let element, points, i, x1, x2, y1, y2, bezier, precision, tolerance, l, cornerThreshold;
	vars = vars || {};
	tolerance = vars.tolerance || 1;
	precision = vars.precision || (1 / tolerance);
	cornerThreshold = (vars.cornerThreshold === undefined ? 18 : +vars.cornerThreshold) * _DEG2RAD;
	if (typeof(data) !== "string") { //element
		element = data;
		data = element.getAttribute("d");
	}
	if (data.charAt(0) === "#" || data.charAt(0) === ".") { //selector text
		element = _doc.querySelector(data);
		if (element) {
			data = element.getAttribute("d");
		}
	}
	points = (vars.curved === false && !/[achqstvz]/ig.test(data)) ? data.match(_numbersExp) : stringToRawPath(data)[0];
	if (vars.curved !== false) {
		bezier = points;
		points = [];
		l = bezier.length;
		for (i = 2; i < l; i+=6) {
			x1 = +bezier[i-2];
			y1 = +bezier[i-1];
			x2 = +bezier[i+4];
			y2 = +bezier[i+5];
			points.push(_round(x1), _round(y1), _round(x2), _round(y2));
			bezierToPoints(x1, y1, +bezier[i], +bezier[i+1], +bezier[i+2], +bezier[i+3], x2, y2, 1 / (precision * 200000), points, points.length - 2);
		}
		points = pointsToSegment(simplifyPoints(points, tolerance), vars.curviness, cornerThreshold);
		points[2] = "C" + points[2];
	} else {
		points = simplifyPoints(points, tolerance);
	}
	data = "M" + points.join(",");
	if (element) {
		element.setAttribute("d", data);
	}
	return data;
};

PathEditor.create = (target, vars) => new PathEditor(target, vars);

PathEditor.editingAxis = _editingAxis;

PathEditor.getSnapFunction = (vars) => { //{gridSize, radius, x, y, width, height}
	let r = vars.radius || 2,
		big = 1e20,
		minX = (vars.x || vars.x === 0) ? vars.x : vars.width ? 0 : -big,
		minY = (vars.y || vars.y === 0) ? vars.y : vars.height ? 0 : -big,
		maxX = minX + (vars.width || big*big),
		maxY = minY + (vars.height || big*big),
		containX = (vars.containX !== false),
		containY = (vars.containY !== false),
		axis = vars.axis,
		grid = vars.gridSize;
	r *= r;
	return p => {
		let x = p.x,
			y = p.y,
			gridX, gridY, dx, dy;
		if ((containX && x < minX) || (dx = x - minX) * dx < r) {
			x = minX;
		} else if ((containX && x > maxX) || (dx = maxX - x) * dx < r) {
			x = maxX;
		}
		if ((containY && y < minY) || (dy = y - minY) * dy < r) {
			y = minY;
		} else if ((containY && y > maxY) || (dy = maxY - y) * dy < r) {
			y = maxY;
		}
		if (axis) {
			dx = x - axis.x;
			dy = y - axis.y;
			if (dx * dx < r) {
				x = axis.x;
			}
			if (dy * dy < r) {
				y = axis.y;
			}
		}
		if (grid) {
			gridX = minX + Math.round((x - minX) / grid) * grid; //closest grid slot on x-axis
			dx = gridX - x;
			gridY = minY + Math.round((y - minY) / grid) * grid; //closest grid slot on y-axis
			dy = gridY - y;
			if (dx * dx + dy * dy < r) {
				x = gridX;
				y = gridY;
			}
		}
		p.x = x;
		p.y = y;
	};
};

PathEditor.version = "3.15.0";

PathEditor.register = _initCore;

export { PathEditor as default };