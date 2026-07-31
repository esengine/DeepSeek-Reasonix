import type { ExtractState, Mutate, StateCreator, StoreApi, StoreMutatorIdentifier } from 'zustand/vanilla';
type ReadonlyStoreApi<T> = Pick<StoreApi<T>, 'getState' | 'getInitialState' | 'subscribe'>;
export declare function useStore<S extends ReadonlyStoreApi<unknown>>(api: S): ExtractState<S>;
export declare function useStore<S extends ReadonlyStoreApi<unknown>, U>(api: S, selector: (state: ExtractState<S>) => U): U;
export type UseBoundStore<S extends ReadonlyStoreApi<unknown>> = {
    (): ExtractState<S>;
    <U>(selector: (state: ExtractState<S>) => U): U;
} & S;
type Create = {
    <T, Mos extends [StoreMutatorIdentifier, unknown][] = []>(initializer: StateCreator<T, [], Mos>): UseBoundStore<Mutate<StoreApi<T>, Mos>>;
    <T>(): <Mos extends [StoreMutatorIdentifier, unknown][] = []>(initializer: StateCreator<T, [], Mos>) => UseBoundStore<Mutate<StoreApi<T>, Mos>>;
};
export declare const create: Create;
export {};
