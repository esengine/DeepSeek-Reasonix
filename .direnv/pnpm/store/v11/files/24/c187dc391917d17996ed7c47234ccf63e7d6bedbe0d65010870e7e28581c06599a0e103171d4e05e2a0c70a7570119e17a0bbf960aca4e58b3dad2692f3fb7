export class GenerationalCache<K, V> {
    constructor(max: number);
    set max(value: number);
    get max(): number;
    get size(): number;
    get(key: K): V | undefined;
    set(key: K, value: V): GenerationalCache<any, any>;
    has(key: K): boolean;
    delete(key: K): boolean;
    clear(): void;
    #private;
}
