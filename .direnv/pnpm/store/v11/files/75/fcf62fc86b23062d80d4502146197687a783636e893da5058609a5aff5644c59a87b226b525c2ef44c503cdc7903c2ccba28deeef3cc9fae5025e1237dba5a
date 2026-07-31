/*!
 * Physics2DPlugin 3.15.0
 * https://gsap.com
 *
 * @license Copyright 2008-2026, GreenSock. All rights reserved.
 * Subject to the terms at https://gsap.com/standard-license
 * @author: Jack Doyle, jack@greensock.com
*/
/* eslint-disable */

let gsap, _coreInitted, _getUnit, _getStyleSaver, _reverting,
	_DEG2RAD = Math.PI / 180,
	_getGSAP = () => gsap || (typeof(window) !== "undefined" && (gsap = window.gsap) && gsap.registerPlugin && gsap),
	_round = value => Math.round(value * 10000) / 10000,
	_bonusValidated = 1, //<name>Physics2DPlugin</name>
	_initCore = core => {
		gsap = core || _getGSAP();
		if (!_coreInitted) {
			_getUnit = gsap.utils.getUnit;
			_getStyleSaver = gsap.core.getStyleSaver;
			_reverting = gsap.core.reverting || function() {};
			_coreInitted = 1;
		}
	};

class PhysicsProp {

	constructor(target, p, velocity, acceleration, stepsPerTimeUnit) {
		let cache = target._gsap,
			curVal = cache.get(target, p);
		this.p = p;
		this.set = cache.set(target, p); //setter
		this.s = this.val = parseFloat(curVal);
		this.u = _getUnit(curVal) || 0;
		this.vel = velocity || 0;
		this.v = this.vel / stepsPerTimeUnit;
		if (acceleration || acceleration === 0) {
			this.acc = acceleration;
			this.a = this.acc / (stepsPerTimeUnit * stepsPerTimeUnit);
		} else {
			this.acc = this.a = 0;
		}
	}

}


export const Physics2DPlugin = {
	version:"3.15.0",
	name:"physics2D",
	register: _initCore,
	init(target, value, tween) {
		_coreInitted || _initCore();
		let data = this,
			angle = +value.angle || 0,
			velocity = +value.velocity || 0,
			acceleration = +value.acceleration || 0,
			xProp = value.xProp || "x",
			yProp = value.yProp || "y",
			aAngle = (value.accelerationAngle || value.accelerationAngle === 0) ? +value.accelerationAngle : angle;
		data.styles = _getStyleSaver && _getStyleSaver(target, value.xProp && value.xProp !== "x" ? value.xProp + "," + value.yProp : "transform");
		data.target = target;
		data.tween = tween;
		data.step = 0;
		data.sps = 30; //steps per second
		if (value.gravity) {
			acceleration = +value.gravity;
			aAngle = 90;
		}
		angle *= _DEG2RAD;
		aAngle *= _DEG2RAD;
		data.fr = 1 - (+value.friction || 0);
		data._props.push(xProp, yProp);

		data.xp = new PhysicsProp(target, xProp, Math.cos(angle) * velocity, Math.cos(aAngle) * acceleration, data.sps);
		data.yp = new PhysicsProp(target, yProp, Math.sin(angle) * velocity, Math.sin(aAngle) * acceleration, data.sps);
		data.skipX = data.skipY = 0;
	},
	render(ratio, data) {
		let { xp, yp, tween, target, step, sps, fr, skipX, skipY } = data,
			time = tween._from ? tween._dur - tween._time : tween._time,
			x, y, tt, steps, remainder, i;
		if (tween._time || !_reverting()) {
			if (fr === 1) {
				tt = time * time * 0.5;
				x = xp.s + xp.vel * time + xp.acc * tt;
				y = yp.s + yp.vel * time + yp.acc * tt;
			} else {
				time *= sps;
				steps = i = (time | 0) - step;
				/*
				Note: rounding errors build up if we walk the calculations backward which we used to do like this to maximize performance:
					i = -i;
					while (i--) {
						xp.val -= xp.v;
						yp.val -= yp.v;
						xp.v /= fr;
						yp.v /= fr;
						xp.v -= xp.a;
						yp.v -= yp.a;
					}
				but now for the sake of accuracy (to ensure rewinding always goes back to EXACTLY the same spot), we force the calculations to go forward every time. So if the tween is going backward, we just start from the beginning and iterate. This is only necessary with friction.
				 */
				if (i < 0) {
					xp.v = xp.vel / sps;
					yp.v = yp.vel / sps;
					xp.val = xp.s;
					yp.val = yp.s;
					data.step = 0;
					steps = i = time | 0;
				}
				remainder = (time % 1) * fr;
				while (i--) {
					xp.v += xp.a;
					yp.v += yp.a;
					xp.v *= fr;
					yp.v *= fr;
					xp.val += xp.v;
					yp.val += yp.v;
				}
				x = xp.val + xp.v * remainder;
				y = yp.val + yp.v * remainder;
				data.step += steps;
			}
			skipX || xp.set(target, xp.p, _round(x) + xp.u);
			skipY || yp.set(target, yp.p, _round(y) + yp.u);
		} else {
			data.styles.revert();
		}
	},
	kill(property) {
		if (this.xp.p === property) {
			this.skipX = 1;
		}
		if (this.yp.p === property) {
			this.skipY = 1;
		}
	}
};


_getGSAP() && gsap.registerPlugin(Physics2DPlugin);

export { Physics2DPlugin as default };