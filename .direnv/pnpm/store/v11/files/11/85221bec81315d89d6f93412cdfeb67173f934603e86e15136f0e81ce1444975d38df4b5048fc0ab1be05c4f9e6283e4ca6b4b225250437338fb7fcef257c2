interface CustomEaseConfig {
  precision?: number;
  height?: number;
  originY?: number;
  width?: number;
  x?: number;
  y?: number;
  invert?: boolean;
  path?: string | Element | ArrayLike<Element>;
}

interface EaseFunction {
  (progress: number): number;
  custom?: CustomEase;
}

declare class CustomEase {
  id: string;
  ease: EaseFunction;
  data: string | number[];
  segment: number[];

  constructor(id: string, data?: string | number[], config?: CustomEaseConfig);
  
  setData(data?: string | number[], config?: CustomEaseConfig): this;
  getSVGData(config?: CustomEaseConfig): string;
  
  static create(id: string, data?: string | number[], config?: CustomEaseConfig): EaseFunction;
  static register(core: object): void;
  static get(id: string): EaseFunction;
  static getSVGData(ease: CustomEase | EaseFunction | string, config?: CustomEaseConfig): string;
}

declare module "gsap/CustomEase" {
  class _CustomEase extends CustomEase {}
  export { _CustomEase as CustomEase };
  export { _CustomEase as default };
}

declare module "gsap/dist/CustomEase" {
  export * from "gsap/CustomEase";
  export { CustomEase as default } from "gsap/CustomEase";
}

declare module "gsap/src/CustomEase" {
  export * from "gsap/CustomEase";
  export { CustomEase as default } from "gsap/CustomEase";
}

declare module "gsap/all" {
  export * from "gsap/CustomEase";
}

