declare namespace gsap {

  type PluginInit = (
    this: PluginScope,
    target: object,
    endValue: any,
    tween: core.Tween,
    index: number,
    targets: object[]
  ) => void;

  type PluginRender = (this: PropTween, progress: number, data: PluginScope) => void;
  type PluginKill = (this: PluginScope) => void;

  interface PluginAliases {
    [key: string]: string;
  }

  // TODO: Incomplete
  interface PropTween {
    _next: PropTween;
    _prev: PropTween;
    d: PluginScope;
    r: PluginRender;
    t: object;
    modifier(...args: any[]): any; // TODO: Add signature
  }
  
  interface PluginScope {
    [key: string]: any;
    _props: string[];
    _pt: PropTween;
    add(...args: any[]): PropTween; // TODO: Add signature
    name: string;
    init: PluginInit;
    kill: PluginKill;
    render: PluginRender;
  }

  interface PluginStatic {
    [key: string]: any;
    targetTest(...args: any[]): any; // TODO: Add signature
    get(target: object, prop: string): any; // TODO: Add signature
    getSetter(...args: any[]): any; // TODO: Add signature
    aliases: PluginAliases;
    register(core: typeof gsap): void;
  }

  interface Plugin extends Partial<PluginStatic> {
    name: string;
    init: PluginInit;
    kill?: PluginKill;
    render?: PluginRender;
  }

  interface AttrVars {
    [key: string]: ArrayValue | TweenValue;
  }

  interface ModifiersVars {
    [key: string]: (value: any, target: any) => any;
  }  

  interface SnapVars {
    [key: string]: number | number[] | { values: number[], radius?: number };
  }

  interface TweenVars {
    attr?: AttrVars;
    modifiers?: ModifiersVars;
    snap?: string | SnapVars;
  }
}

declare namespace gsap.plugins {

  interface AttrPlugin extends Plugin {}
  interface ModifiersPlugin extends Plugin {}
  interface SnapPlugin extends Plugin {}

  interface AttrPluginClass extends AttrPlugin {
    new(): PluginScope & AttrPlugin;
    prototype: PluginScope & AttrPlugin;
  }

  interface ModifiersPluginClass extends ModifiersPlugin {
    new(): PluginScope & ModifiersPlugin;
    prototype: PluginScope & ModifiersPlugin;
  }

  interface SnapPluginClass extends SnapPlugin {
    new(): PluginScope & SnapPlugin;
    prototype: PluginScope & SnapPlugin;
  }

  const attr: AttrPluginClass;
  const modifiers: ModifiersPluginClass;
  const snap: SnapPluginClass;

  // Data types shared between plugins
  type RawPath = number[][]; 
  type Matrix2D = { a: number, b: number, c: number, d: number, e: number, f: number};
}

declare const AttrPlugin: gsap.plugins.AttrPlugin;
declare const ModifiersPlugin: gsap.plugins.ModifiersPlugin;
declare const SnapPlugin: gsap.plugins.SnapPlugin;
