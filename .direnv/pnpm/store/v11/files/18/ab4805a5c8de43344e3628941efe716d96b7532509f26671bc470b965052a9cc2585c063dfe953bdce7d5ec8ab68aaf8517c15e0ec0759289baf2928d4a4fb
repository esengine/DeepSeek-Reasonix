import type { ExtractState, Mutate, StateCreator, StoreApi, StoreMutatorIdentifier } from 'zustand/vanilla';
type ReadonlyStoreApi<T> = Pick<StoreApi<T>, 'getState' | 'getInitialState' | 'subscribe'>;
export declare function useStoreWithEqualityFn<S extends ReadonlyStoreApi<unknown>>(api: S): ExtractState<S>;
export declare function useStoreWithEqualityFn<S extends ReadonlyStoreApi<unknown>, U>(api: S, selector: (state: ExtractState<S>) => U, equalityFn?: (a: U, b: U) => boolean): U;
export type UseBoundStoreWithEqualityFn<S extends ReadonlyStoreApi<unknown>> = {
    (): ExtractState<S>;
    <U>(selector: (state: ExtractState<S>) => U, equalityFn?: (a: U, b: U) => boolean): U;
} & S;
type CreateWithEqualityFn = {
    <T, Mos extends [StoreMutatorIdentifier, unknown][] = []>(initializer: StateCreator<T, [], Mos>, defaultEqualityFn?: <U>(a: U, b: U) => boolean): UseBoundStoreWithEqualityFn<Mutate<StoreApi<T>, Mos>>;
    <T>(): <Mos extends [StoreMutatorIdentifier, unknown][] = []>(initializer: StateCreator<T, [], Mos>, defaultEqualityFn?: <U>(a: U, b: U) => boolean) => UseBoundStoreWithEqualityFn<Mutate<StoreApi<T>, Mos>>;
};
export declare const createWithEqualityFn: CreateWithEqualityFn;
export {};
