/*!
 * ScrollSmoother 3.15.0
 * https://gsap.com
 *
 * @license Copyright 2008-2026, GreenSock. All rights reserved.
 * Subject to the terms at https://gsap.com/standard-license
 * @author: Jack Doyle, jack@greensock.com
*/
/* eslint-disable */

let gsap, _coreInitted, _win, _doc, _docEl, _body, _root, _toArray, _clamp, ScrollTrigger, _mainInstance, _expo, _getVelocityProp, _inputObserver, _context, _onResizeDelayedCall,
	_windowExists = () => typeof(window) !== "undefined",
	_getGSAP = () => gsap || (_windowExists() && (gsap = window.gsap) && gsap.registerPlugin && gsap),
	_bonusValidated = 1, //<name>ScrollSmoother</name>
	_round = value => Math.round(value * 100000) / 100000 || 0,
	_maxScroll = scroller => ScrollTrigger.maxScroll(scroller || _win),
	_autoDistance = (el, progress) => { // for calculating the distance (and offset) for elements with speed: "auto". Progress is for if it's "above the fold" (negative start position), so we can crop as little as possible.
		let parent = el.parentNode || _docEl,
			b1 = el.getBoundingClientRect(),
			b2 = parent.getBoundingClientRect(),
			gapTop = b2.top - b1.top,
			gapBottom = b2.bottom - b1.bottom,
			change = (Math.abs(gapTop) > Math.abs(gapBottom) ? gapTop : gapBottom) / (1 - progress),
			offset = -change * progress,
			ratio, extraChange;
		if (change > 0) { // if the image starts at the BOTTOM of the container, adjust things so that it shows as much of the image as possible while still covering.
			ratio = b2.height / (_win.innerHeight + b2.height);
			extraChange = ratio === 0.5 ? b2.height * 2 : Math.min(b2.height, Math.abs(-change * ratio / (2 * ratio - 1))) * 2 * (progress || 1);
			offset += progress ? -extraChange * progress : -extraChange / 2; // whatever the offset, we must double that in the opposite direction to compensate.
			change += extraChange;
		}
		return {change, offset};
	},
	_wrap = el => {
		let wrapper = _doc.querySelector(".ScrollSmoother-wrapper"); // some frameworks load multiple times, so one already exists, just use that to avoid duplicates
		if (!wrapper) {
			wrapper = _doc.createElement("div");
			wrapper.classList.add("ScrollSmoother-wrapper");
			el.parentNode.insertBefore(wrapper, el);
			wrapper.appendChild(el);
		}
		return wrapper;
	};

export class ScrollSmoother {

	constructor(vars) {
		_coreInitted || ScrollSmoother.register(gsap) || console.warn("Please gsap.registerPlugin(ScrollSmoother)");
		vars = this.vars = vars || {};

		_mainInstance && _mainInstance.kill();
		_mainInstance = this;
		_context(this);

		let {smoothTouch, onUpdate, onStop, smooth, onFocusIn, normalizeScroll, wholePixels} = vars,
			content, wrapper, height, mainST, effects, sections, intervalID, wrapperCSS, contentCSS, paused, pausedNormalizer, recordedRefreshScroll, recordedRefreshScrub, allowUpdates,
			self = this,
			effectsPrefix = vars.effectsPrefix || "",
			scrollFunc = ScrollTrigger.getScrollFunc(_win),
			smoothDuration = ScrollTrigger.isTouch === 1 ? (smoothTouch === true ? 0.8 : parseFloat(smoothTouch) || 0) : (smooth === 0 || smooth === false) ? 0 : parseFloat(smooth) || 0.8,
			speed = (smoothDuration && +vars.speed) || 1,
			currentY = 0,
			delta = 0,
			startupPhase = 1,
			tracker = _getVelocityProp(0),
			updateVelocity = () => tracker.update(-currentY),
			scroll = {y: 0},
			removeScroll = () => content.style.overflow = "visible",
			isProxyScrolling,
			killScrub = trigger => {
				trigger.update(); // it's possible that it hasn't been synchronized with the actual scroll position yet, like if it's later in the _triggers Array. If it was already updated, it'll skip the processing anyway.
				let scrub = trigger.getTween();
				if (scrub) {
					scrub.pause();
					scrub._time = scrub._dur; // force the playhead to completion without rendering just so that when it resumes, it doesn't jump back in the .resetTo().
					scrub._tTime = scrub._tDur;
				}
				isProxyScrolling = false;
				trigger.animation.progress(trigger.progress, true);
			},
			render = (y, force) => {
				if ((y !== currentY && !paused) || force) {
					wholePixels && (y = Math.round(y));
					if (smoothDuration) {
						content.style.transform = "matrix3d(1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, " + y + ", 0, 1)";
						//content.style.transform = "translateY(" + y + "px)"; // NOTE: when we used matrix3d() or set will-change: transform, it performed noticeably worse on iOS counter-intuitively!
						content._gsap.y = y + "px";
					}
					delta = y - currentY;
					currentY = y;
					ScrollTrigger.isUpdating || ScrollSmoother.isRefreshing || ScrollTrigger.update(); // note: if we allowed an update() when in the middle of a refresh() it could render all the other ScrollTriggers and inside the update(), _refreshing would be true thus scrubs would jump instantly, but then on the very next update they'd continue from there. Basically this allowed update() to be called on OTHER ScrollTriggers during the refresh() of the mainST which could cause some complications. See https://gsap.com/forums/topic/35536-smoothscroller-ignoremobileresize-for-non-touch-devices
				}
			},
			scrollTop = function(value) {
				if (arguments.length) {
					(value < 0) && (value = 0);
					scroll.y = -value; // don't use currentY because we must accurately track the delta variable (in render() method)
					isProxyScrolling = true; // otherwise, if snapping was applied (or anything that attempted to SET the scroll proxy's scroll position), we'd set the scroll here which would then (on the next tick) update the content tween/ScrollTrigger which would try to smoothly animate to that new value, thus the scrub tween would impede the progress. So we use this flag to respond accordingly in the ScrollTrigger's onUpdate and effectively force the scrub to its end immediately.
					paused ? (currentY = -value) : render(-value);
					ScrollTrigger.isRefreshing ? mainST.update() : scrollFunc(value / speed); // during a refresh, we revert all scrollers to 0 and then put them back. We shouldn't force the window to that value too during the refresh.
					return this;
				}
				return -currentY;
			},
			resizeObserver = typeof(ResizeObserver) !== "undefined" && vars.autoResize !== false && new ResizeObserver(() => {
				if (!ScrollTrigger.isRefreshing) {
					let max = _maxScroll(wrapper) * speed;
					max < -currentY && scrollTop(max) // if the user scrolled down to the bottom, for example, and then the page resizes smaller, we should adjust things accordingly right away so that the scroll position isn't past the very end.
					_onResizeDelayedCall.restart(true);
				}
			}),
			lastFocusElement, // if the user clicks a button that scrolls the page, for example, then unfocuses the window and comes back and activates the window/tab again, it'll want to focus back on that same button element but in that case we should skip it. Only jump there when a new element gets focus, like tabbing for accessibility.
			_onFocusIn = e => { // when the focus changes, make sure that element is on-screen
				wrapper.scrollTop = 0;
				if ((e.target.contains && e.target.contains(wrapper)) || (onFocusIn && onFocusIn(this, e) === false)) {
					return;
				}
				ScrollTrigger.isInViewport(e.target) || (e.target === lastFocusElement) ||  this.scrollTo(e.target, false, "center center");
				lastFocusElement = e.target;
			},
			_transformPosition = (position, st) => { // feed in a position (start or end scroll value) and a ScrollTrigger that's associated with a parallax effect and it'll spit back the adjusted position based on the movement of the trigger. For example, if the trigger goes at a speed of 0.5 while in the viewport, we must push the start/end values of OTHER ScrollTriggers that use that same trigger further down to compensate.
				if (position < st.start) {
					return position;
				}
				let ratio = isNaN(st.ratio) ? 1 : st.ratio,
					change = st.end - st.start,
					distance = position - st.start,
					offset = st.offset || 0,
					pins = st.pins || [],
					pinOffset = pins.offset || 0,
					progressOffset = (st._startClamp && st.start <= 0) || (st.pins && st.pins.offset) ? 0 : (st._endClamp && st.end === _maxScroll()) ? 1 : 0.5;

				pins.forEach(p => { // remove any pinning space/distance
					change -= p.distance;
					if (p.nativeStart <= position) {
						distance -= p.distance;
					}
				});
				if (pinOffset) { // edge case when a clamped effect starts mid-pin; we've gotta compensate for the smaller change amount (the yOffset gets set to the st.pins.offset, so let's say it clamps such that the page starts with the element pinned 100px in, we have to set the yOffset to 100 but then subtract 100 from the change value to compensate, thus we must scale the positions accordingly based on the ratios. Like if it would normally have a change of 2000, and a pin would normally hit at 1000, but we're offsetting by 100, that means everything must scale now that we're only moving 1900px rather than 2000px.
					distance *= (change - pinOffset / ratio) / change;
				}
				return position + (distance - offset * progressOffset) / ratio - distance;
			},
			adjustEffectRelatedTriggers = (st, triggers, partial) => { // if we're using this method to do only a partial Array of triggers, we should NOT reset or rebuild the pin data. For example, we tap into this from the offset() method.
				partial || (st.pins.length = st.pins.offset = 0);
				let pins = st.pins,
					markers = st.markers,
					dif, isClamped, start, end, nativeStart, nativeEnd, i, trig;
				for (i = 0; i < triggers.length; i++) {
					trig = triggers[i];
					if (st.trigger && trig.trigger && st !== trig && (trig.trigger === st.trigger || trig.pinnedContainer === st.trigger || st.trigger.contains(trig.trigger))) {
						nativeStart = trig._startNative || trig._startClamp || trig.start;
						nativeEnd = trig._endNative || trig._endClamp || trig.end;
						start = _transformPosition(nativeStart, st);
						// note: _startClamp and _endClamp are populated with the unclamped values. For the sake of efficiency sake, we use the property both like a boolean to indicate that clamping is enabled AND the actual original unclamped value which we need in situations like if there's a data-speed="" on an element that has something like start="clamp(top bottom)". For in-viewport elements, it would clamp the values on the ScrollTrigger first, then feed it here and we'd adjust it on the clamped value which could throw things off - we need to apply the logic to the unclamped value and THEN re-apply clamping on the result.
						end = (trig.pin && nativeEnd > 0) ? start + (nativeEnd - nativeStart) : _transformPosition(nativeEnd, st);
						trig.setPositions(start, end, true, (trig._startClamp ? Math.max(0, start) : start) - nativeStart); // the last value (pinOffset) is to adjust the pinStart y value inside ScrollTrigger to accommodate for the y offset that gets applied by the parallax effect.
						trig.markerStart && markers.push(gsap.quickSetter([trig.markerStart, trig.markerEnd], "y", "px"));
						if (trig.pin && trig.end > 0 && !partial) {
							dif = trig.end - trig.start;
							isClamped = (st._startClamp && trig.start < 0);
							if (isClamped) {
								if (st.start > 0) { // the trigger element on the effect must have been pinned BEFORE its starting position, so in this edge case we must adjust the start position to be 0 and end position to get pushed further by the amount of the overlap
									st.setPositions(0, st.end + (st._startNative - st.start), true); // add the overlap amount
									adjustEffectRelatedTriggers(st, triggers);
									return; // start over for this trigger element!
								}
								dif += trig.start;
								pins.offset = -trig.start; // edge case when a clamped effect starts mid-pin, we've gotta compensate in the onUpdate algorithm.
							}
							pins.push({start: trig.start, nativeStart, end: trig.end, distance: dif, trig: trig});
							st.setPositions(st.start, st.end + (isClamped ? -trig.start : dif), true);
						}
					}
				}
			},
			adjustParallaxPosition = (triggers, createdAfterEffectWasApplied) => {
				effects.forEach(st => adjustEffectRelatedTriggers(st, triggers, createdAfterEffectWasApplied));
			},
			onRefresh = () => {
				_docEl = _doc.documentElement; // some frameworks like Astro may cache the <body> and replace it during routing, so we'll just re-record the _docEl and _body for safety (otherwise, the markers may not get added properly).
				_body = _doc.body;
				removeScroll();
				requestAnimationFrame(removeScroll);
				if (effects) { // adjust all the effect start/end positions including any pins!
					ScrollTrigger.getAll().forEach(st => { // record the native start/end positions because we'll be messing with them and need a way to have a "source of truth"
						st._startNative = st.start;
						st._endNative = st.end;
					});
					effects.forEach(st => {
						let start = st._startClamp || st.start, // if it was already clamped, we should base things on the unclamped value and then do the clamping here.
							end = st.autoSpeed ? Math.min(_maxScroll(), st.end) : start + Math.abs((st.end - start) / st.ratio),
							offset = end - st.end; // we split the difference so that it reaches its natural position in the MIDDLE of the viewport
						start -= offset / 2;
						end -= offset / 2;
						if (start > end) {
							let s = start;
							start = end;
							end = s;
						}
						if (st._startClamp && start < 0) {
							end = st.ratio < 0 ? _maxScroll() : st.end / st.ratio;
							offset = end - st.end;
							start = 0;
						} else if (st.ratio < 0 || (st._endClamp && end >= _maxScroll())) {
							end = _maxScroll();
							start = st.ratio < 0 ? 0 : st.ratio > 1 ? 0 : end - (end - st.start) / st.ratio;
							offset = (end - start) * st.ratio - (st.end - st.start);
						}
						st.offset = offset || 0.0001; // we assign at least a tiny value because we check in the onUpdate for .offset being set in order to apply values.
						st.pins.length = st.pins.offset = 0;
						st.setPositions(start, end, true);
						// note: another way of getting only the amount of offset traveled for a certain ratio is: distanceBetweenStartAndEnd * (1 / ratio - 1)
					});
					adjustParallaxPosition(ScrollTrigger.sort());
				}
				tracker.reset();
			},
			addOnRefresh = () => ScrollTrigger.addEventListener("refresh", onRefresh),
			restoreEffects = () => effects && effects.forEach(st => st.vars.onRefresh(st)),
			revertEffects = () => {
				effects && effects.forEach(st => st.vars.onRefreshInit(st));
				return restoreEffects;
			},
			effectValueGetter = (name, value, index, el) => {
				return () => {
					let v = typeof(value) === "function" ? value(index, el) : value;
					v || v === 0 || (v = el.getAttribute("data-" + effectsPrefix + name) || (name === "speed" ? 1 : 0));
					el.setAttribute("data-" + effectsPrefix + name, v);
					let clamp = (v + "").substr(0, 6) === "clamp(";
					return {clamp, value: clamp ? v.substr(6, v.length - 7) : v};
				};
			},
			createEffect = (el, speed, lag, index, effectsPadding) => {
				effectsPadding = (typeof(effectsPadding) === "function" ? effectsPadding(index, el) : effectsPadding) || 0;
				let getSpeed = effectValueGetter("speed", speed, index, el),
					getLag = effectValueGetter("lag", lag, index, el),
					startY = gsap.getProperty(el, "y"),
					cache = el._gsap,
					ratio, st, autoSpeed, scrub, progressOffset, yOffset,
					pins = [],
					initDynamicValues = () => {
						speed = getSpeed();
						lag = parseFloat(getLag().value);
						ratio = parseFloat(speed.value) || 1;
						autoSpeed = speed.value === "auto";
						progressOffset = autoSpeed || (st && st._startClamp && st.start <= 0) || pins.offset ? 0 : (st && st._endClamp && st.end === _maxScroll()) ? 1 : 0.5;
						scrub && scrub.kill();
						scrub = lag && gsap.to(el, {ease: _expo, overwrite: false, y: "+=0", duration: lag});
						if (st) {
							st.ratio = ratio;
							st.autoSpeed = autoSpeed;
						}
					},
					revert = () => {
						cache.y = startY + "px";
						cache.renderTransform(1);
						initDynamicValues();
					},
					markers = [],
					change = 0,
					updateChange = self => {
						if (autoSpeed) {
							revert();
							let auto = _autoDistance(el, _clamp(0, 1, -self.start / (self.end - self.start)));
							change = auto.change;
							yOffset = auto.offset;
						} else {
							yOffset = pins.offset || 0;
							change = (self.end - self.start - yOffset) * (1 - ratio);
						}
						pins.forEach(p => change -= p.distance * (1 - ratio));
						self.offset = change || 0.001;
						self.vars.onUpdate(self);
						scrub && scrub.progress(1);
					};
				initDynamicValues();
				if (ratio !== 1 || autoSpeed || scrub) {
					st = ScrollTrigger.create({
						trigger: autoSpeed ? el.parentNode : el,
						start: () => speed.clamp ? "clamp(top bottom+=" + effectsPadding + ")" : "top bottom+=" + effectsPadding,
						end: () => speed.value < 0 ? "max" : speed.clamp ? "clamp(bottom top-=" + effectsPadding + ")" : "bottom top-=" + effectsPadding,
						scroller: wrapper,
						scrub: true,
						refreshPriority: -999, // must update AFTER any other ScrollTrigger pins
						onRefreshInit: revert,
						onRefresh: updateChange,
						onKill: self => {
							let i = effects.indexOf(self);
							i >= 0 && effects.splice(i, 1);
							revert();
						},
						onUpdate: self => {
							let y = startY + change * (self.progress - progressOffset),
								i = pins.length,
								extraY = 0,
								pin, scrollY, end;
							if (self.offset) { // wait until the effects are adjusted.
								if (i) { // pinning must be handled in a special way because when pinned, slope changes to 1.
									scrollY = -currentY; // -scroll.y;
									end = self.end;
									while (i--) {
										pin = pins[i];
										if (pin.trig.isActive || (scrollY >= pin.start && scrollY <= pin.end)) { // currently pinned so no need to set anything
											if (scrub) {
												pin.trig.progress += pin.trig.direction < 0 ? 0.001 : -0.001; // just to make absolutely sure that it renders (if the progress didn't change, it'll skip)
												pin.trig.update(0, 0, 1);
												scrub.resetTo("y", parseFloat(cache.y), -delta, true);
												startupPhase && scrub.progress(1);
											}
											return;
										}
										(scrollY > pin.end) && (extraY += pin.distance);
										end -= pin.distance;
									}
									y = startY + extraY + change * (((gsap.utils.clamp(self.start, self.end, scrollY) - self.start - extraY) / (end - self.start)) - progressOffset);
								}
								markers.length && !autoSpeed && markers.forEach(setter => setter(y - extraY));
								y = _round(y + yOffset);
								if (scrub) {
									scrub.resetTo("y", y, -delta, true);
									startupPhase && scrub.progress(1);
								} else {
									cache.y = y + "px";
									cache.renderTransform(1);
								}
							}
						}
					});
					updateChange(st);
					gsap.core.getCache(st.trigger).stRevert = revertEffects; // if user calls ScrollSmoother.create() with effects and THEN creates a ScrollTrigger on the same trigger element, the effect would throw off the start/end positions thus we needed a way to revert things when creating a new ScrollTrigger in that scenario, so we use this stRevert property of the GSCache inside ScrollTrigger.
					st.startY = startY;
					st.pins = pins;
					st.markers = markers;
					st.ratio = ratio;
					st.autoSpeed = autoSpeed;
					el.style.willChange = "transform";
				}
				return st;
			};

		addOnRefresh();
		ScrollTrigger.addEventListener("killAll", addOnRefresh);
		gsap.delayedCall(0.5, () => startupPhase = 0);

		this.scrollTop = scrollTop;

		this.scrollTo = (target, smooth, position) => {
			let p = gsap.utils.clamp(0, _maxScroll(), isNaN(target) ? this.offset(target, position, !!smooth && !paused) : +target);
			!smooth ? scrollTop(p) : paused ? gsap.to(this, {duration: smoothDuration, scrollTop: p, overwrite: "auto", ease: _expo}) : scrollFunc(p);
		};

		this.offset = (target, position, ignoreSpeed) => {
			target = _toArray(target)[0];
			let cssText = target.style.cssText, // because if there's an effect applied, we revert(). We need to restore.
				st = ScrollTrigger.create({trigger: target, start: position || "top top"}),
				y;
			if (effects) {
				startupPhase ? ScrollTrigger.refresh() : adjustParallaxPosition([st], true); // all the effects need to go through the initial full refresh() so that all the pins and ratios and offsets are set up. That's why we do a full refresh() if it's during the startupPhase.
			}
			y = st.start / (ignoreSpeed ? speed : 1);
			st.kill(false);
			target.style.cssText = cssText;
			gsap.core.getCache(target).uncache = 1;
			return y;
		};

		function refreshHeight() {
			height = content.clientHeight;
			content.style.overflow = "visible"
			_body.style.height = (_win.innerHeight + (height - _win.innerHeight) / speed) + "px";
			return (height - _win.innerHeight);
		}

		this.content = function(element) {
			if (arguments.length) {
				let newContent = _toArray(element || "#smooth-content")[0] || console.warn("ScrollSmoother needs a valid content element.") || _body.children[0];
				if (newContent !== content) {
					content = newContent;
					contentCSS = content.getAttribute("style") || "";
					resizeObserver && resizeObserver.observe(content);
					gsap.set(content, {overflow: "visible", width: "100%", boxSizing: "border-box", y: "+=0"});
					smoothDuration || gsap.set(content, {clearProps: "transform"});
				}
				return this;
			}
			return content;
		}

		this.wrapper = function(element) {
			if (arguments.length) {
				wrapper = _toArray(element || "#smooth-wrapper")[0] || _wrap(content);
				wrapperCSS = wrapper.getAttribute("style") || "";
				refreshHeight();
				gsap.set(wrapper, smoothDuration ? {overflow: "hidden", position: "fixed", height: "100%", width: "100%", top: 0, left: 0, right: 0, bottom: 0} : {overflow: "visible", position: "relative", width: "100%", height: "auto", top: "auto", bottom: "auto", left: "auto", right: "auto"});
				return this;
			}
			return wrapper;
		}

		this.effects = (targets, config) => {
			effects || (effects = []);
			if (!targets) {
				return effects.slice(0);
			}
			targets = _toArray(targets);
			targets.forEach(target => {
				let i = effects.length;
				while (i--) {
					effects[i].trigger === target && effects[i].kill(); // will automatically splice() it from the effects Array in the onKill
				}
			});
			config = config || {};
			let {speed, lag, effectsPadding} = config,
				effectsToAdd = [],
				i, st;
			for (i = 0; i < targets.length; i++) {
				st = createEffect(targets[i], speed, lag, i, effectsPadding);
				st && effectsToAdd.push(st);
			}
			effects.push(...effectsToAdd);
			config.refresh !== false && ScrollTrigger.refresh(); // certain effects require a refresh to work properly
			return effectsToAdd;
		};

		this.sections = (targets, config) => {
			sections || (sections = []);
			if (!targets) {
				return sections.slice(0);
			}
			let newSections = _toArray(targets).map(el => ScrollTrigger.create({
					trigger: el,
					start: "top 120%",
					end: "bottom -20%",
					onToggle: self => {
						el.style.opacity = self.isActive ? "1" : "0";
						el.style.pointerEvents = self.isActive ? "all" : "none";
					}
				})
			);
			config && config.add ? sections.push(...newSections) : (sections = newSections.slice(0));
			return newSections;
		}

		this.content(vars.content);
		this.wrapper(vars.wrapper);
		this.render = y => render(y || y === 0 ? y : currentY);
		this.getVelocity = () => tracker.getVelocity(-currentY);

		ScrollTrigger.scrollerProxy(wrapper, {
			scrollTop: scrollTop,
			scrollHeight: () => refreshHeight() && _body.scrollHeight,
			fixedMarkers: vars.fixedMarkers !== false && !!smoothDuration,
			content: content,
			getBoundingClientRect() {
				return {top: 0, left: 0, width: _win.innerWidth, height: _win.innerHeight};
			}
		});
		ScrollTrigger.defaults({scroller: wrapper});
		let existingScrollTriggers = ScrollTrigger.getAll().filter(st => st.scroller === _win || st.scroller === wrapper);
		existingScrollTriggers.forEach(st => st.revert(true, true)); // in case it's in an environment like React where child components that have ScrollTriggers instantiate BEFORE the parent that does ScrollSmoother.create(...);

		mainST = ScrollTrigger.create({
			animation: gsap.fromTo(scroll, {y: () => { allowUpdates = 0; return 0;}}, {
				y: () => {allowUpdates = 1; return -refreshHeight();},
				immediateRender: false,
				ease: "none",
				data: "ScrollSmoother",
				duration: 100, // for added precision
				onUpdate: function() {
					if (allowUpdates) { // skip when it's the "from" part of the tween (setting the startAt)
						let force = isProxyScrolling;
						if (force) {
							killScrub(mainST);
							scroll.y = currentY;
						}
						render(scroll.y, force);
						updateVelocity();
						onUpdate && !paused && onUpdate(self);
					}
				}
			}),
			onRefreshInit: self => {
				if (ScrollSmoother.isRefreshing) { // gets called on the onRefresh() when we do self.setPositions(...) in which case we should skip this
					return;
				}
				ScrollSmoother.isRefreshing = true;
				if (effects) {
					let pins = ScrollTrigger.getAll().filter(st => !!st.pin);
					effects.forEach(st => {
						if (!st.vars.pinnedContainer) {
							pins.forEach(pinST => {
								if (pinST.pin.contains(st.trigger)) {
									let v = st.vars;
									v.pinnedContainer = pinST.pin;
									st.vars = null; // otherwise, it'll self.kill(), triggering the onKill()
									st.init(v, st.animation);
								}
							});
						}
					});
				}
				let scrub = self.getTween();
				recordedRefreshScrub = scrub && scrub._end > scrub._dp._time; // don't use scrub.progress() < 1 because we may have called killScrub() recently in which case it'll report progress() as 1 when we were actually in the middle of a scrub. That's why we tap into the _end instead.
				recordedRefreshScroll = currentY;
				scroll.y = 0;
				if (smoothDuration) {
					ScrollTrigger.isTouch === 1 && (wrapper.style.position = "absolute"); // Safari 16 has a major bug - if you set wrapper.scrollTop to 0 (even if it's already 0), it blocks the whole page from scrolling page non-scrollable! See https://bugs.webkit.org/show_bug.cgi?id=245300 and https://codepen.io/GreenSock/pen/YzLZVOz. Originally we set pointer-events: none on the wrapper temporarily, and set it back to all after setting scrollTop to 0, but that could cause mouseenter/mouseleave/etc. events to fire too, so we opted to set the position to absolute and then back to fixed after setting scrollTop.
					wrapper.scrollTop = 0; // set wrapper.scrollTop to 0 because in some very rare situations, the browser will auto-set that, like if there's a hash in the link or changing focus to an off-screen input
					ScrollTrigger.isTouch === 1 && (wrapper.style.position = "fixed");
				}
			},
			onRefresh: self => {
				self.animation.invalidate(); // because pinnedContainers may have been found in ScrollTrigger's _refreshAll() that extend the height. Without this, it may prevent the user from being able to scroll all the way down.
				scroll.y = 0; // since we invalidated the tween, we must reset scroll.y to 0, otherwise when the tween inits it'll record the current scroll.y as the pre-fromTo(), thus when ScrollTrigger calls revert(), it'll set it back to that old value! See https://gsap.com/community/forums/topic/45138-scrollsmoother-stops-and-scrolltrigger-misaligned-in-browser-dev-tools/
				self.setPositions(self.start, refreshHeight() / speed);
				recordedRefreshScrub || killScrub(self);
				scroll.y = -scrollFunc() * speed; // in 3.11.1, we shifted to forcing the scroll position to 0 during the entire refreshAll() in ScrollTrigger and then restored the scroll position AFTER everything had been updated, thus we should always make these adjustments AFTER a full refresh rather than putting it in the onRefresh() of the individual mainST ScrollTrigger which would fire before the scroll position was restored.
				render(scroll.y);
				if (!startupPhase) {
					recordedRefreshScrub && (isProxyScrolling = false); // otherwise, we lose any in-progress scrub. When we set the progress(), it fires the onUpdate() which sets the scroll position immediately (jumps ahead if isProxyScrolling is true). See https://gsap.com/community/forums/topic/37515-dynamic-scrolltrigger-with-pin-inside-a-scrollsmoother/
					self.animation.progress(gsap.utils.clamp(0, 1, recordedRefreshScroll / speed / -self.end));
				}
				if (recordedRefreshScrub) { // we need to trigger the scrub to happen again
					self.progress -= 0.001;
					self.update();
				}
				ScrollSmoother.isRefreshing = false;
			},
			id: "ScrollSmoother",
			scroller: _win,
			invalidateOnRefresh: true,
			start: 0,
			refreshPriority: -9999, // because all other pins, etc. should be calculated first before this figures out the height of the body. BUT this should also update FIRST so that the scroll position on the proxy is up-to-date when all the ScrollTriggers calculate their progress! -9999 is a special number that ScrollTrigger looks for to handle in this way.
			end: () => refreshHeight() / speed,
			onScrubComplete: () => {
				tracker.reset();
				onStop && onStop(this);
			},
			scrub: smoothDuration || true,
		});

		this.smooth = function(value) {
			if (arguments.length) {
				smoothDuration = value || 0;
				speed = (smoothDuration && +vars.speed) || 1;
				mainST.scrubDuration(value);
			}
			return mainST.getTween() ? mainST.getTween().duration() : 0;
		};

		mainST.getTween() && (mainST.getTween().vars.ease = vars.ease || _expo);

		this.scrollTrigger = mainST;

		vars.effects && this.effects(vars.effects === true ? "[data-" + effectsPrefix + "speed], [data-" + effectsPrefix + "lag]" : vars.effects, {effectsPadding: vars.effectsPadding, refresh: false});
		vars.sections && this.sections(vars.sections === true ? "[data-section]" : vars.sections);

		existingScrollTriggers.forEach(st => {
			st.vars.scroller = wrapper;
			st.revert(false, true);
			st.init(st.vars, st.animation);
		});

		this.paused = function(value, allowNestedScroll) {
			if (arguments.length) {
				if (!!paused !== value) {
					if (value) { // pause
						mainST.getTween() && mainST.getTween().pause();
						scrollFunc(-currentY / speed);
						tracker.reset();
						pausedNormalizer = ScrollTrigger.normalizeScroll();
						pausedNormalizer && pausedNormalizer.disable(); // otherwise the normalizer would try to scroll the page on things like wheel events.
						paused = ScrollTrigger.observe({
							preventDefault: true,
							type: "wheel,touch,scroll",
							debounce: false,
							allowClicks: true,
							onChangeY: () => scrollTop(-currentY) // refuse to scroll
						});
						paused.nested = _inputObserver(_docEl, "wheel,touch,scroll", true, allowNestedScroll !== false); // allow nested scrolling, like modals
					} else { // resume
						paused.nested.kill();
						paused.kill();
						paused = 0;
						pausedNormalizer && pausedNormalizer.enable();
						mainST.progress = (-currentY / speed - mainST.start) / (mainST.end - mainST.start);
						killScrub(mainST);
					}
				}
				return this;
			}
			return !!paused;
		};

		this.kill = this.revert = () => {
			this.paused(false);
			killScrub(mainST);
			mainST.kill();
			let triggers = (effects || []).concat(sections || []),
				i = triggers.length;
			while (i--) { // make sure we go backwards because the onKill() will effects.splice(index, 1) and we don't want to skip
				triggers[i].kill();
			}
			ScrollTrigger.scrollerProxy(wrapper);
			ScrollTrigger.removeEventListener("killAll", addOnRefresh);
			ScrollTrigger.removeEventListener("refresh", onRefresh);
			wrapper.style.cssText = wrapperCSS;
			content.style.cssText = contentCSS;
			let defaults = ScrollTrigger.defaults({});
			defaults && defaults.scroller === wrapper && ScrollTrigger.defaults({scroller: _win});
			this.normalizer && ScrollTrigger.normalizeScroll(false);
			clearInterval(intervalID);
			_mainInstance = null;
			resizeObserver && resizeObserver.disconnect();
			_body.style.removeProperty("height");
			_win.removeEventListener("focusin", _onFocusIn);
		}

		this.refresh = (soft, force) => mainST.refresh(soft, force);

		if (normalizeScroll) {
			this.normalizer = ScrollTrigger.normalizeScroll(normalizeScroll === true ? { debounce: true, content: !smoothDuration && content } : normalizeScroll);
		}

		ScrollTrigger.config(vars); // in case user passes in ignoreMobileResize for example
		// ("overscrollBehavior" in _win.getComputedStyle(_body)) && gsap.set([_body, _docEl], {overscrollBehavior: "none"}); // this caused Safari 17+ not to scroll the entire page (bug in Safari), so let people set this in the CSS instead if they want.
		("scrollBehavior" in _win.getComputedStyle(_body)) && gsap.set([_body, _docEl], {scrollBehavior: "auto"});

		// if the user hits the tab key (or whatever) to shift focus to an element that's off-screen, center that element.
		_win.addEventListener("focusin", _onFocusIn);

		intervalID = setInterval(updateVelocity, 250);

		_doc.readyState === "loading" || requestAnimationFrame(() => ScrollTrigger.refresh());

	}

	get progress() {
		return this.scrollTrigger ? this.scrollTrigger.animation._time / 100 : 0;
	}


	static register(core) {
		if (!_coreInitted) {
			gsap = core || _getGSAP();
			if (_windowExists() && window.document) {
				_win = window;
				_doc = document;
				_docEl = _doc.documentElement;
				_body = _doc.body;
			}
			if (gsap) {
				_toArray = gsap.utils.toArray;
				_clamp = gsap.utils.clamp;
				_expo = gsap.parseEase("expo");
				_context = gsap.core.context || function() {};
				ScrollTrigger = gsap.core.globals().ScrollTrigger;
				gsap.core.globals("ScrollSmoother", ScrollSmoother); // must register the global manually because in Internet Explorer, functions (classes) don't have a "name" property.
				if (_body && ScrollTrigger) {
					_onResizeDelayedCall = gsap.delayedCall(0.2, () => ScrollTrigger.isRefreshing || (_mainInstance && _mainInstance.refresh())).pause();
					_root = [_win, _doc, _docEl, _body];
					_getVelocityProp = ScrollTrigger.core._getVelocityProp;
					_inputObserver = ScrollTrigger.core._inputObserver;
					ScrollSmoother.refresh = ScrollTrigger.refresh;
					_coreInitted = 1;
				}
			}
		}
		return _coreInitted;
	}


}

ScrollSmoother.version = "3.15.0";
ScrollSmoother.create = vars => (_mainInstance && vars && _mainInstance.content() === _toArray(vars.content)[0]) ? _mainInstance : new ScrollSmoother(vars);
ScrollSmoother.get = () => _mainInstance;

_getGSAP() && gsap.registerPlugin(ScrollSmoother);

export { ScrollSmoother as default };