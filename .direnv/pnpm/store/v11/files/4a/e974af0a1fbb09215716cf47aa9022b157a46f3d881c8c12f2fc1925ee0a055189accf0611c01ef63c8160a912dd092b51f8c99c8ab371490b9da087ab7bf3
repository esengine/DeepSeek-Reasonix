/*!
 * CSSRulePlugin 3.15.0
 * https://gsap.com
 * 
 * @license Copyright 2026, GreenSock. All rights reserved.
 * Subject to the terms at https://gsap.com/standard-license.
 * @author: Jack Doyle, jack@greensock.com
 */

!function(e,t){"object"==typeof exports&&"undefined"!=typeof module?t(exports):"function"==typeof define&&define.amd?define(["exports"],t):t((e=e||self).window=e.window||{})}(this,function(e){"use strict";function h(){return"undefined"!=typeof window}function i(){return t||h()&&(t=window.gsap)&&t.registerPlugin&&t}function j(){return n||(s(),o||console.warn("Please gsap.registerPlugin(CSSPlugin, CSSRulePlugin)")),n}var t,n,c,o,s=function _initCore(e){t=e||i(),h()&&(c=document),t&&(o=t.plugins.css)&&(n=1)},r={version:"3.15.0",name:"cssRule",init:function init(e,t,n,i,s){if(!j()||void 0===e.cssText)return!1;var r=e._gsProxy=e._gsProxy||c.createElement("div");this.ss=e,this.style=r.style,r.style.cssText=e.cssText,o.prototype.init.call(this,r,t,n,i,s)},render:function render(e,t){for(var n,i=t._pt,s=t.style,r=t.ss;i;)i.r(e,i.d),i=i._next;for(n=s.length;-1<--n;)r[s[n]]=s[s[n]]},getRule:function getRule(e){j();var t,n,i,s,r=c.all?"rules":"cssRules",o=c.styleSheets,l=o.length,u=":"===e.charAt(0);for(e=(u?"":",")+e.split("::").join(":").toLowerCase()+",",u&&(s=[]);l--;){try{if(!(n=o[l][r]))continue;t=n.length}catch(e){console.warn(e);continue}for(;-1<--t;)if((i=n[t]).selectorText&&-1!==(","+i.selectorText.split("::").join(":").toLowerCase()+",").indexOf(e)){if(!u)return i.style;s.push(i.style)}}return s},register:s};i()&&t.registerPlugin(r),e.CSSRulePlugin=r,e.default=r;if (typeof(window)==="undefined"||window!==e){Object.defineProperty(e,"__esModule",{value:!0})} else {delete e.default}});

