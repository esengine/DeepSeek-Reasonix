type SetStateInternal<T> = {
    _(partial: T | Partial<T> | {
        _(state: T): T | Partial<T>;
    }['_'], replace?: false): void;
    _(state: T | {
        _(state: T): T;
    }['_'], replace: true): void;
}['_'];
export interface StoreApi<T> {
    setState: SetStateInternal<T>;
    getState: () => T;
    getInitialState: () => T;
    subscribe: (listener: (state: T, prevState: T) => void) => () => void;
}
export type ExtractState<S> = S extends {
    getState: () => infer T;
} ? T : never;
type Get<T, K, F> = K extends keyof T ? T[K] : F;
export type Mutate<S, Ms> = number extends Ms['length' & keyof Ms] ? S : Ms extends [] ? S : Ms extends [[infer Mi, infer Ma], ...infer Mrs] ? Mutate<StoreMutators<S, Ma>[Mi & StoreMutatorIdentifier], Mrs> : never;
export type StateCreator<T, Mis extends [StoreMutatorIdentifier, unknown][] = [], Mos extends [StoreMutatorIdentifier, unknown][] = [], U = T> = ((setState: Get<Mutate<StoreApi<T>, Mis>, 'setState', never>, getState: Get<Mutate<StoreApi<T>, Mis>, 'getState', never>, store: Mutate<StoreApi<T>, Mis>) => U) & {
    $$storeMutators?: Mos;
};
export interface StoreMutators<S, A> {
}
export type StoreMutatorIdentifier = keyof StoreMutators<unknown, unknown>;
type CreateStore = {
    <T, Mos extends [StoreMutatorIdentifier, unknown][] = []>(initializer: StateCreator<T, [], Mos>): Mutate<StoreApi<T>, Mos>;
    <T>(): <Mos extends [StoreMutatorIdentifier, unknown][] = []>(initializer: StateCreator<T, [], Mos>) => Mutate<StoreApi<T>, Mos>;
};
export declare const createStore: CreateStore;
export {};
