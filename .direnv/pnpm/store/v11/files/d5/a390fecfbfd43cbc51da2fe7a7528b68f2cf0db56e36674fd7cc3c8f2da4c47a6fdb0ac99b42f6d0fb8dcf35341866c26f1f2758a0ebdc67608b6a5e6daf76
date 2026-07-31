declare namespace gsap {

  type VelocityType = "num" | "deg" | "rad";

  interface VelocityMap {
    [key: string]: number;
  }

  interface VelocityTrackerInstance {
    readonly target: object;
    add(property: string, type?: VelocityType): void;
    kill(shallow?: boolean): void;
    remove(property: string): void;
    getAll(): VelocityMap;
    get(property: string): number;
  }

  interface VelocityTrackerStatic {
    getByTarget(target: TweenTarget): VelocityTrackerInstance;
    getVelocity(target: TweenTarget, property: string): number;
    isTracking(target: TweenTarget, property?: string): boolean;
    track(target: TweenTarget, properties: string, type?: VelocityType): VelocityTrackerInstance[];
    untrack(target: TweenTarget, properties?: string): void;
  }

  interface VelocityTracker extends VelocityTrackerStatic {
    new(target: TweenTarget, properties?: string, type?: VelocityType, next?: VelocityTrackerInstance): VelocityTrackerInstance;
    prototype: VelocityTrackerInstance;
    register(core: typeof gsap): void;
  }
}

declare const VelocityTracker: gsap.VelocityTracker;

declare module "gsap/utils/VelocityTracker" {
  export const VelocityTracker: gsap.VelocityTracker;
  export { VelocityTracker as default };
}

declare module "gsap/src/utils/VelocityTracker" {
  export * from "gsap/utils/VelocityTracker";
  export { VelocityTracker as default } from "gsap/utils/VelocityTracker";
}

declare module "gsap-trial/utils/VelocityTracker" {
  export * from "gsap/utils/VelocityTracker";
  export { VelocityTracker as default } from "gsap/utils/VelocityTracker";
}

declare module "gsap-trial/src/utils/VelocityTracker" {
  export * from "gsap/utils/VelocityTracker";
  export { VelocityTracker as default } from "gsap/utils/VelocityTracker";
}
