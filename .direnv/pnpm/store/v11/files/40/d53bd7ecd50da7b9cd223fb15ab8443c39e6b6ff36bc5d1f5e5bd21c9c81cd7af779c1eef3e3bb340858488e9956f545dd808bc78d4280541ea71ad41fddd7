import type { StateCreator, StoreApi, StoreMutatorIdentifier } from 'zustand/vanilla';
type Config = Parameters<(Window extends {
    __REDUX_DEVTOOLS_EXTENSION__?: infer T;
} ? T : {
    connect: (param: any) => unknown;
})['connect']>[0];
declare module '../vanilla.mjs' {
    interface StoreMutators<S, A> {
        'zustand/devtools': WithDevtools<S>;
    }
}
type Cast<T, U> = T extends U ? T : U;
type Write<T, U> = Omit<T, keyof U> & U;
type TakeTwo<T> = T extends {
    length: 0;
} ? [undefined, undefined] : T extends {
    length: 1;
} ? [...args0: Cast<T, unknown[]>, arg1: undefined] : T extends {
    length: 0 | 1;
} ? [...args0: Cast<T, unknown[]>, arg1: undefined] : T extends {
    length: 2;
} ? T : T extends {
    length: 1 | 2;
} ? T : T extends {
    length: 0 | 1 | 2;
} ? T : T extends [infer A0, infer A1, ...unknown[]] ? [A0, A1] : T extends [infer A0, (infer A1)?, ...unknown[]] ? [A0, A1?] : T extends [(infer A0)?, (infer A1)?, ...unknown[]] ? [A0?, A1?] : never;
type WithDevtools<S> = Write<S, StoreDevtools<S>>;
type Action = string | {
    type: string;
    [x: string | number | symbol]: unknown;
};
type StoreDevtools<S> = S extends {
    setState: {
        (...args: infer Sa1): infer Sr1;
        (...args: infer Sa2): infer Sr2;
    };
} ? {
    setState(...args: [...args: TakeTwo<Sa1>, action?: Action]): Sr1;
    setState(...args: [...args: TakeTwo<Sa2>, action?: Action]): Sr2;
    devtools: {
        cleanup: () => void;
    };
} : never;
export interface DevtoolsOptions extends Config {
    name?: string;
    enabled?: boolean;
    anonymousActionType?: string;
    store?: string;
}
type Devtools = <T, Mps extends [StoreMutatorIdentifier, unknown][] = [], Mcs extends [StoreMutatorIdentifier, unknown][] = [], U = T>(initializer: StateCreator<T, [...Mps, ['zustand/devtools', never]], Mcs, U>, devtoolsOptions?: DevtoolsOptions) => StateCreator<T, Mps, [['zustand/devtools', never], ...Mcs], U>;
export type NamedSet<T> = WithDevtools<StoreApi<T>>['setState'];
export declare const devtools: Devtools;
export {};
