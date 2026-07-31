/*!
 * EasePack 3.15.0
 * https://gsap.com
 *
 * @license Copyright 2008-2026, GreenSock. All rights reserved.
 * Subject to the terms at https://gsap.com/standard-license
 * @author: Jack Doyle, jack@greensock.com
*/
/* eslint-disable */

let gsap, _coreInitted, _registerEase,
	_getGSAP = () => gsap || (typeof(window) !== "undefined" && (gsap = window.gsap) && gsap.registerPlugin && gsap),
	_boolean = (value, defaultValue) => !!(typeof(value) === "undefined" ? defaultValue : value && !~((value + "").indexOf("false"))),
	_initCore = core => {
		gsap = core || _getGSAP();
		if (gsap) {
			_registerEase = gsap.registerEase;
			//add weighted ease capabilities to standard eases so users can do "power2.inOut(0.8)" for example to push everything toward the "out", or (-0.8) to push it toward the "in" (0 is neutral)
			let eases = gsap.parseEase(),
				createConfig = ease => ratio => {
					let y = 0.5 + ratio / 2;
					ease.config = p => ease(2 * (1 - p) * p * y + p * p);
				},
				p;
			for (p in eases) {
				if (!eases[p].config) {
					createConfig(eases[p]);
				}
			}
			_registerEase("slow", SlowMo);
			_registerEase("expoScale", ExpoScaleEase);
			_registerEase("rough", RoughEase);
			for (p in EasePack) {
				p !== "version" && gsap.core.globals(p, EasePack[p]);
			}
			_coreInitted = 1;
		}
	},
	_createSlowMo = (linearRatio, power, yoyoMode) => {
		linearRatio = Math.min(1, linearRatio || 0.7);
		let pow = linearRatio < 1 ? ((power || power === 0) ? power : 0.7) : 0,
			p1 = (1 - linearRatio) / 2,
			p3 = p1 + linearRatio,
			calcEnd = _boolean(yoyoMode);
		return p => {
			let r = p + (0.5 - p) * pow;
			return (p < p1) ? (calcEnd ? 1 - ((p = 1 - (p / p1)) * p) : r - ((p = 1 - (p / p1)) * p * p * p * r)) : (p > p3) ? (calcEnd ? (p === 1 ? 0 : 1 - (p = (p - p3) / p1) * p) : r + ((p - r) * (p = (p - p3) / p1) * p * p * p)) : (calcEnd ? 1 : r);
		}
	},
	_createExpoScale = (start, end, ease) => {
		let p1 = Math.log(end / start),
			p2 = end - start;
		ease && (ease = gsap.parseEase(ease));
		return p => (start * Math.exp(p1 * (ease ? ease(p) : p)) - start) / p2;
	},
	EasePoint = function(time, value, next) {
		this.t = time;
		this.v = value;
		if (next) {
			this.next = next;
			next.prev = this;
			this.c = next.v - value;
			this.gap = next.t - time;
		}
	},
	_createRoughEase = vars => {
		if (typeof(vars) !== "object") { //users may pass in via a string, like "rough(30)"
			vars = {points: +vars || 20};
		}
		let taper = vars.taper || "none",
			a = [],
			cnt = 0,
			points = (+vars.points || 20) | 0,
			i = points,
			randomize = _boolean(vars.randomize, true),
			clamp = _boolean(vars.clamp),
			template = gsap ? gsap.parseEase(vars.template) : 0,
			strength = (+vars.strength || 1) * 0.4,
			x, y, bump, invX, obj, pnt, recent;
		while (--i > -1) {
			x = randomize ? Math.random() : (1 / points) * i;
			y = template ? template(x) : x;
			if (taper === "none") {
				bump = strength;
			} else if (taper === "out") {
				invX = 1 - x;
				bump = invX * invX * strength;
			} else if (taper === "in") {
				bump = x * x * strength;
			} else if (x < 0.5) {  //"both" (start)
				invX = x * 2;
				bump = invX * invX * 0.5 * strength;
			} else {				//"both" (end)
				invX = (1 - x) * 2;
				bump = invX * invX * 0.5 * strength;
			}
			if (randomize) {
				y += (Math.random() * bump) - (bump * 0.5);
			} else if (i % 2) {
				y += bump * 0.5;
			} else {
				y -= bump * 0.5;
			}
			if (clamp) {
				if (y > 1) {
					y = 1;
				} else if (y < 0) {
					y = 0;
				}
			}
			a[cnt++] = {x:x, y:y};
		}
		a.sort((a, b) => a.x - b.x);
		pnt = new EasePoint(1, 1, null);
		i = points;
		while (i--) {
			obj = a[i];
			pnt = new EasePoint(obj.x, obj.y, pnt);
		}
		recent = new EasePoint(0, 0, pnt.t ? pnt : pnt.next);
		return p => {
			let pnt = recent;
			if (p > pnt.t) {
				while (pnt.next && p >= pnt.t) {
					pnt = pnt.next;
				}
				pnt = pnt.prev;
			} else {
				while (pnt.prev && p <= pnt.t) {
					pnt = pnt.prev;
				}
			}
			recent = pnt;
			return pnt.v + ((p - pnt.t) / pnt.gap) * pnt.c;
		};
	};

export const SlowMo = _createSlowMo(0.7);
SlowMo.ease = SlowMo; //for backward compatibility
SlowMo.config = _createSlowMo;

export const ExpoScaleEase = _createExpoScale(1, 2);
ExpoScaleEase.config = _createExpoScale;

export const RoughEase = _createRoughEase();
RoughEase.ease = RoughEase; //for backward compatibility
RoughEase.config = _createRoughEase;

export const EasePack = {
	SlowMo: SlowMo,
	RoughEase: RoughEase,
	ExpoScaleEase: ExpoScaleEase
};

for (let p in EasePack) {
	EasePack[p].register = _initCore;
	EasePack[p].version = "3.15.0";
}

_getGSAP() && gsap.registerPlugin(SlowMo);

export { EasePack as default };