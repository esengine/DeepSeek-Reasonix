/*!
 * @gsap/react 2.1.2
 * https://gsap.com
 *
 * @license Copyright 2025, GreenSock. All rights reserved.
 * Subject to the terms at https://gsap.com/standard-license or for Club GSAP members, the agreement issued with that membership.
 * @author: Jack Doyle, jack@greensock.com
 */

!function(e,t){"object"==typeof exports&&"undefined"!=typeof module?t(exports,require("react"),require("gsap")):"function"==typeof define&&define.amd?define(["exports","react","gsap"],t):t((e=e||self).window=e.window||{},e.react,e.gsap)}(this,function(e,f,t){"use strict";t=t&&Object.prototype.hasOwnProperty.call(t,"default")?t.default:t;let i="undefined"!=typeof document?f.useLayoutEffect:f.useEffect,a=e=>e&&!Array.isArray(e)&&"object"==typeof e,p=[],l={},y=t;t=(e,t=p)=>{let n=l;a(e)?(n=e,e=null,t="dependencies"in n?n.dependencies:p):a(t)&&(t="dependencies"in(n=t)?n.dependencies:p),e&&"function"!=typeof e&&console.warn("First parameter must be a function or config object");const{scope:r,revertOnUpdate:c}=n,o=f.useRef(!1),u=f.useRef(y.context(()=>{},r)),s=f.useRef(e=>u.current.add(null,e)),d=t&&t.length&&!c;return d&&i(()=>(o.current=!0,()=>u.current.revert()),p),i(()=>{if(e&&u.current.add(e,r),!d||!o.current)return()=>u.current.revert()},t),{context:u.current,contextSafe:s.current}};t.register=e=>{y=e},t.headless=!0,e.useGSAP=t,Object.defineProperty(e,"__esModule",{value:!0})});
