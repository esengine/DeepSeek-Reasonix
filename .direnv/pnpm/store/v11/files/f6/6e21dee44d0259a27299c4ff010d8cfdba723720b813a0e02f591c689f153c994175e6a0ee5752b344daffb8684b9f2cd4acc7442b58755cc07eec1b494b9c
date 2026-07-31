import type { StateCreator, StoreMutatorIdentifier } from 'zustand/vanilla';
export interface StateStorage<R = unknown> {
    getItem: (name: string) => string | null | Promise<string | null>;
    setItem: (name: string, value: string) => R;
    removeItem: (name: string) => R;
}
export type StorageValue<S> = {
    state: S;
    version?: number;
};
export interface PersistStorage<S, R = unknown> {
    getItem: (name: string) => StorageValue<S> | null | Promise<StorageValue<S> | null>;
    setItem: (name: string, value: StorageValue<S>) => R;
    removeItem: (name: string) => R;
}
type JsonStorageOptions = {
    reviver?: (key: string, value: unknown) => unknown;
    replacer?: (key: string, value: unknown) => unknown;
};
export declare function createJSONStorage<S, R = unknown>(getStorage: () => StateStorage<R>, options?: JsonStorageOptions): PersistStorage<S, unknown> | undefined;
export interface PersistOptions<S, PersistedState = S, PersistReturn = unknown> {
    /** Name of the storage (must be unique) */
    name: string;
    /**
     * Use a custom persist storage.
     *
     * Combining `createJSONStorage` helps creating a persist storage
     * with JSON.parse and JSON.stringify.
     *
     * @default createJSONStorage(() => window.localStorage)
     */
    storage?: PersistStorage<PersistedState, PersistReturn> | undefined;
    /**
     * Filter the persisted value.
     *
     * @params state The state's value
     */
    partialize?: (state: S) => PersistedState;
    /**
     * A function returning another (optional) function.
     * The main function will be called before the state rehydration.
     * The returned function will be called after the state rehydration or when an error occurred.
     */
    onRehydrateStorage?: (state: S) => ((state?: S, error?: unknown) => void) | void;
    /**
     * If the stored state's version mismatch the one specified here, the storage will not be used.
     * This is useful when adding a breaking change to your store.
     */
    version?: number;
    /**
     * A function to perform persisted state migration.
     * This function will be called when persisted state versions mismatch with the one specified here.
     */
    migrate?: (persistedState: unknown, version: number) => PersistedState | Promise<PersistedState>;
    /**
     * A function to perform custom hydration merges when combining the stored state with the current one.
     * By default, this function does a shallow merge.
     */
    merge?: (persistedState: unknown, currentState: S) => S;
    /**
     * An optional boolean that will prevent the persist middleware from triggering hydration on initialization,
     * This allows you to call `rehydrate()` at a specific point in your apps rendering life-cycle.
     *
     * This is useful in SSR application.
     *
     * @default false
     */
    skipHydration?: boolean;
}
type PersistListener<S> = (state: S) => void;
type StorePersist<S, Ps, Pr> = S extends {
    getState: () => infer T;
    setState: {
        (...args: infer Sa1): infer Sr1;
        (...args: infer Sa2): infer Sr2;
    };
} ? {
    setState(...args: Sa1): Sr1 | Pr;
    setState(...args: Sa2): Sr2 | Pr;
    persist: {
        setOptions: (options: Partial<PersistOptions<T, Ps, Pr>>) => void;
        clearStorage: () => void;
        rehydrate: () => Promise<void> | void;
        hasHydrated: () => boolean;
        onHydrate: (fn: PersistListener<T>) => () => void;
        onFinishHydration: (fn: PersistListener<T>) => () => void;
        getOptions: () => Partial<PersistOptions<T, Ps, Pr>>;
    };
} : never;
type Persist = <T, Mps extends [StoreMutatorIdentifier, unknown][] = [], Mcs extends [StoreMutatorIdentifier, unknown][] = [], U = T>(initializer: StateCreator<T, [...Mps, ['zustand/persist', unknown]], Mcs>, options: PersistOptions<T, U>) => StateCreator<T, Mps, [['zustand/persist', U], ...Mcs]>;
declare module '../vanilla.mjs' {
    interface StoreMutators<S, A> {
        'zustand/persist': WithPersist<S, A>;
    }
}
type Write<T, U> = Omit<T, keyof U> & U;
type WithPersist<S, A> = Write<S, StorePersist<S, A, unknown>>;
export declare const persist: Persist;
export {};
