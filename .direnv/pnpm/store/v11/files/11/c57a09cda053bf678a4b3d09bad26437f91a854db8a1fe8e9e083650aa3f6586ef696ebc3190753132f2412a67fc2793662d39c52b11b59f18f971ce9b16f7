/*!
 * @gsap/react 2.1.2
 * https://gsap.com
 *
 * @license Copyright 2025, GreenSock. All rights reserved.
 * Subject to the terms at https://gsap.com/standard-license or for Club GSAP members, the agreement issued with that membership.
 * @author: Jack Doyle, jack@greensock.com
 */

(function (global, factory) {
  typeof exports === 'object' && typeof module !== 'undefined' ? factory(exports, require('react'), require('gsap')) :
  typeof define === 'function' && define.amd ? define(['exports', 'react', 'gsap'], factory) :
  (global = global || self, factory(global.window = global.window || {}, global.react, global.gsap));
}(this, (function (exports, react, gsap) { 'use strict';

  gsap = gsap && Object.prototype.hasOwnProperty.call(gsap, 'default') ? gsap['default'] : gsap;

  let useIsomorphicLayoutEffect = typeof document !== "undefined" ? react.useLayoutEffect : react.useEffect,
    isConfig = value => value && !Array.isArray(value) && typeof value === "object",
    emptyArray = [],
    defaultConfig = {},
    _gsap = gsap;
  const useGSAP = (callback, dependencies = emptyArray) => {
    let config = defaultConfig;
    if (isConfig(callback)) {
      config = callback;
      callback = null;
      dependencies = "dependencies" in config ? config.dependencies : emptyArray;
    } else if (isConfig(dependencies)) {
      config = dependencies;
      dependencies = "dependencies" in config ? config.dependencies : emptyArray;
    }
    callback && typeof callback !== "function" && console.warn("First parameter must be a function or config object");
    const {
        scope,
        revertOnUpdate
      } = config,
      mounted = react.useRef(false),
      context = react.useRef(_gsap.context(() => {}, scope)),
      contextSafe = react.useRef(func => context.current.add(null, func)),
      deferCleanup = dependencies && dependencies.length && !revertOnUpdate;
    deferCleanup && useIsomorphicLayoutEffect(() => {
      mounted.current = true;
      return () => context.current.revert();
    }, emptyArray);
    useIsomorphicLayoutEffect(() => {
      callback && context.current.add(callback, scope);
      if (!deferCleanup || !mounted.current) {
        return () => context.current.revert();
      }
    }, dependencies);
    return {
      context: context.current,
      contextSafe: contextSafe.current
    };
  };
  useGSAP.register = core => {
    _gsap = core;
  };
  useGSAP.headless = true;

  exports.useGSAP = useGSAP;

  if (typeof(window) === 'undefined' || window !== exports) {Object.defineProperty(exports, '__esModule', { value: true });} else {delete window.default;}

})));
