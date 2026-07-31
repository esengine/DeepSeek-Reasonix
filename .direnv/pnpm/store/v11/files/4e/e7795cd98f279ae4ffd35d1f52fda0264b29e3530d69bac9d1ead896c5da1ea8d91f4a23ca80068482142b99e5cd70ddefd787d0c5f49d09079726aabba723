interface CustomBounceVars {
    strength?: number;
    endAtStart?: boolean;
    squash?: number;
    squashID?: string;
}

interface EaseFunction {
    (progress: number): number;
}

declare class CustomBounce {
    ease: EaseFunction;

    constructor(id: string, vars?: CustomBounceVars);

    static create(id: string, vars?: CustomBounceVars): EaseFunction;
    static register(core: object): void;
}

declare module "gsap/CustomBounce" {
    class _CustomBounce extends CustomBounce {}
    export { _CustomBounce as CustomBounce };
    export { _CustomBounce as default };
}

declare module "gsap/dist/CustomBounce" {
    export * from "gsap/CustomBounce";
    export { CustomBounce as default } from "gsap/CustomBounce";
}

declare module "gsap/src/CustomBounce" {
    export * from "gsap/CustomBounce";
    export { CustomBounce as default } from "gsap/CustomBounce";
}

declare module "gsap/all" {
    export * from "gsap/CustomBounce";
}