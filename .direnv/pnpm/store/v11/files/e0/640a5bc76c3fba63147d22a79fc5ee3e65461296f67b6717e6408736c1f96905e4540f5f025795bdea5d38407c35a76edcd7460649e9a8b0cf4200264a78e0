import type { Draft } from 'immer';
import type { StateCreator, StoreMutatorIdentifier } from 'zustand/vanilla';
type Immer = <T, Mps extends [StoreMutatorIdentifier, unknown][] = [], Mcs extends [StoreMutatorIdentifier, unknown][] = [], U = T>(initializer: StateCreator<T, [...Mps, ['zustand/immer', never]], Mcs, U>) => StateCreator<T, Mps, [['zustand/immer', never], ...Mcs], U>;
declare module '../vanilla.mjs' {
    interface StoreMutators<S, A> {
        ['zustand/immer']: WithImmer<S>;
    }
}
type Write<T, U> = Omit<T, keyof U> & U;
type SkipTwo<T> = T extends {
    length: 0;
} ? [] : T extends {
    length: 1;
} ? [] : T extends {
    length: 0 | 1;
} ? [] : T extends [unknown, unknown, ...infer A] ? A : T extends [unknown, unknown?, ...infer A] ? A : T extends [unknown?, unknown?, ...infer A] ? A : never;
type SetStateType<T extends unknown[]> = Exclude<T[0], (...args: any[]) => any>;
type WithImmer<S> = Write<S, StoreImmer<S>>;
type StoreImmer<S> = S extends {
    setState: infer SetState;
} ? SetState extends {
    (...args: infer A1): infer Sr1;
    (...args: infer A2): infer Sr2;
} ? {
    setState(nextStateOrUpdater: SetStateType<A2> | Partial<SetStateType<A2>> | ((state: Draft<SetStateType<A2>>) => void), shouldReplace?: false, ...args: SkipTwo<A1>): Sr1;
    setState(nextStateOrUpdater: SetStateType<A2> | ((state: Draft<SetStateType<A2>>) => void), shouldReplace: true, ...args: SkipTwo<A2>): Sr2;
} : never : never;
export declare const immer: Immer;
export {};
