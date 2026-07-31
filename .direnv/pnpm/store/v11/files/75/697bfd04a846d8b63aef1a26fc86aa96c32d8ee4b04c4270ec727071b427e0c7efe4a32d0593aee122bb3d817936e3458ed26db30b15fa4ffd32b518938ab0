/*!
 * @gsap/react 2.1.2
 * https://gsap.com
 *
 * Copyright 2008-2025, GreenSock. All rights reserved.
 * Subject to the terms at https://gsap.com/standard-license or for
 * Club GSAP members, the agreement issued with that membership.
 * @author: Jack Doyle, jack@greensock.com
*/
/* eslint-disable */
import { useEffect, useLayoutEffect, useRef } from "react";
import gsap from "gsap";

let useIsomorphicLayoutEffect = typeof document !== "undefined" ? useLayoutEffect : useEffect,
    isConfig = value => value && !Array.isArray(value) && typeof(value) === "object",
    emptyArray = [],
    defaultConfig = {},
    _gsap = gsap; // accommodates situations where different versions of GSAP may be loaded, so a user can gsap.registerPlugin(useGSAP);

export const useGSAP = (callback, dependencies = emptyArray) => {
  let config = defaultConfig;
  if (isConfig(callback)) {
    config = callback;
    callback = null;
    dependencies = "dependencies" in config ? config.dependencies : emptyArray;
  } else if (isConfig(dependencies)) {
    config = dependencies;
    dependencies = "dependencies" in config ? config.dependencies : emptyArray;
  }
  (callback && typeof callback !== "function") && console.warn("First parameter must be a function or config object");
  const { scope, revertOnUpdate } = config,
        mounted = useRef(false),
        context = useRef(_gsap.context(() => { }, scope)),
        contextSafe = useRef((func) => context.current.add(null, func)),
        deferCleanup = dependencies && dependencies.length && !revertOnUpdate;
  deferCleanup && useIsomorphicLayoutEffect(() => {
    mounted.current = true;
    return () => context.current.revert();
  }, emptyArray);
  useIsomorphicLayoutEffect(() => {
    callback && context.current.add(callback, scope);
    if (!deferCleanup || !mounted.current) { // React renders bottom-up, thus there could be hooks with dependencies that run BEFORE the component mounts, thus cleanup wouldn't occur since a hook with an empty dependency Array would only run once the component mounts.
      return () => context.current.revert();
    }
  }, dependencies);
  return { context: context.current, contextSafe: contextSafe.current };
};
useGSAP.register = core => { _gsap = core; };
useGSAP.headless = true; // doesn't require the window to be registered.
