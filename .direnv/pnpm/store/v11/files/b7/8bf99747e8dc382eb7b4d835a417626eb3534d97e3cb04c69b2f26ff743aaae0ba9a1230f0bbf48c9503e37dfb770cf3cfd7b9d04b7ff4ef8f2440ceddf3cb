/*!
 * PhysicsPropsPlugin 3.15.0
 * https://gsap.com
 * 
 * @license Copyright 2026, GreenSock. All rights reserved.
 * Subject to the terms at https://gsap.com/standard-license.
 * @author: Jack Doyle, jack@greensock.com
 */

!function(e,t){"object"==typeof exports&&"undefined"!=typeof module?t(exports):"function"==typeof define&&define.amd?define(["exports"],t):t((e=e||self).window=e.window||{})}(this,function(e){"use strict";function i(){return t||"undefined"!=typeof window&&(t=window.gsap)&&t.registerPlugin&&t}function j(e){return Math.round(1e4*e)/1e4}function k(e){t=e||i(),p||(a=t.utils.getUnit,c=t.core.getStyleSaver,d=t.core.reverting||function(){},p=1)}function l(e,t,s,i,r,n){var o=e._gsap,f=o.get(e,t);this.p=t,this.set=o.set(e,t),this.s=this.val=parseFloat(f),this.u=a(f)||0,this.vel=s||0,this.v=this.vel/n,i||0===i?(this.acc=i,this.a=this.acc/(n*n)):this.acc=this.a=0,this.fr=1-(r||0)}var t,p,a,c,d,s={version:"3.15.0",name:"physicsProps",register:k,init:function init(e,t,s){p||k();var i,r=this;for(i in r.styles=c&&c(e),r.target=e,r.tween=s,r.step=0,r.sps=30,r.vProps=[],t){var n=t[i],o=n.velocity,f=n.acceleration,a=n.friction;(o||f)&&(r.vProps.push(new l(e,i,o,f,a,r.sps)),r._props.push(i),c&&r.styles.save(i),a&&(r.hasFr=1))}},render:function render(e,t){var s,i,r,n,o,f=t.vProps,l=t.tween,a=t.target,p=t.step,c=t.hasFr,u=t.sps,v=f.length,h=l._from?l._dur-l._time:l._time;if(l._time||!d())if(c){if((i=(0|(h*=u))-p)<0){for(;v--;)(s=f[v]).v=s.vel/u,s.val=s.s;v=f.length,t.step=p=0,i=0|h}for(r=h%1;v--;){for(s=f[v],n=i;n--;)s.v+=s.a,s.v*=s.fr,s.val+=s.v;s.set(a,s.p,j(s.val+s.v*r*s.fr)+s.u)}t.step+=i}else for(o=h*h*.5;v--;)(s=f[v]).set(a,s.p,j(s.s+s.vel*h+s.acc*o)+s.u);else t.styles.revert()},kill:function kill(e){for(var t=this.vProps,s=t.length;s--;)t[s].p===e&&t.splice(s,1)}};i()&&t.registerPlugin(s),e.PhysicsPropsPlugin=s,e.default=s;if (typeof(window)==="undefined"||window!==e){Object.defineProperty(e,"__esModule",{value:!0})} else {delete e.default}});

