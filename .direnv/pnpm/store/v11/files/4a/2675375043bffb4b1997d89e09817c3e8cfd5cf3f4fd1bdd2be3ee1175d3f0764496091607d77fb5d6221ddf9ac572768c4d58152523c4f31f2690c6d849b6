declare namespace gsap {

  interface TweenVars {
    cssRule?: object; // TODO make more specific
  }
}

declare namespace gsap.plugins {

  interface CSSRulePlugin extends Plugin {
    /**
     * Gets the style sheet object associated with a particular selector.
     * 
     * ```js
     * var rule = CSSRulePlugin.getRule(".myClass::before");
     * // Then do what you want with it, such as:
     * gsap.to(rule, { duration: 3, cssRule: { color: "#0000FF" } });
     * ```
     *
     * @param {string} selector
     * @returns {CSSRule} The CSSRule
     * @memberof CSSRulePlugin
     * @link https://greensock.com/docs/v3/Plugins/CSSRulePlugin
     */
    getRule(selector: string): CSSRule;
  }

  interface CSSRulePluginClass extends CSSRulePlugin {
    new(): PluginScope & CSSRulePlugin;
    prototype: PluginScope & CSSRulePlugin;
  }

  const cssRule: CSSRulePluginClass;
}

declare const CSSRulePlugin: gsap.plugins.CSSRulePlugin;

declare module "gsap/CSSRulePlugin" {
  export const CSSRulePlugin: gsap.plugins.CSSRulePlugin;
  export { CSSRulePlugin as default };
}

declare module "gsap/src/CSSRulePlugin" {
  export * from "gsap/CSSRulePlugin";
  export { CSSRulePlugin as default } from "gsap/CSSRulePlugin";
}

declare module "gsap/dist/CSSRulePlugin" {
  export * from "gsap/CSSRulePlugin";
  export { CSSRulePlugin as default } from "gsap/CSSRulePlugin";
}

declare module "gsap/all" {
  export * from "gsap/CSSRulePlugin";
}

declare module "gsap-trial/CSSRulePlugin" {
  export * from "gsap/CSSRulePlugin";
  export { CSSRulePlugin as default } from "gsap/CSSRulePlugin";
}

declare module "gsap-trial/src/CSSRulePlugin" {
  export * from "gsap/CSSRulePlugin";
  export { CSSRulePlugin as default } from "gsap/CSSRulePlugin";
}

declare module "gsap-trial/dist/CSSRulePlugin" {
  export * from "gsap/CSSRulePlugin";
  export { CSSRulePlugin as default } from "gsap/CSSRulePlugin";
}

declare module "gsap-trial/all" {
  export * from "gsap/CSSRulePlugin";
}
