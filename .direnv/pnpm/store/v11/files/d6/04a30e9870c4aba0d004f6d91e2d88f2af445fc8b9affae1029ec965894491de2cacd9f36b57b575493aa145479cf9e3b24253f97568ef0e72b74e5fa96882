/**
 * @typedef {{ type: 'hmr:module-registered', modules: string[] }} DevRuntimeMessage
 * @typedef {{ send(message: DevRuntimeMessage): void }} Messenger
 */
export class DevRuntime {
    /**
     * @param {Messenger} messenger
     * @param {string} clientId
     */
    constructor(messenger: Messenger, clientId: string);
    /**
     * Client ID generated at runtime initialization, used for lazy compilation requests.
     * @type {string}
     */
    clientId: string;
    messenger: Messenger;
    /**
     * @type {Record<string, Module>}
     */
    modules: Record<string, Module>;
    /**
     * @param {string} _moduleId
     */
    createModuleHotContext(_moduleId: string): void;
    /**
     * @param {[string, string][]} _boundaries
     */
    applyUpdates(_boundaries: [string, string][]): void;
    /**
     * @param {string} id
     * @param {{ exports: any }} exportsHolder
     */
    registerModule(id: string, exportsHolder: {
        exports: any;
    }): void;
    /**
     * @param {string} id
     */
    loadExports(id: string): any;
    /**
     * __esmMin
     *
     * When `dedup` is truthy and `id` is already registered on the runtime,
     * skip the factory: another lazy blob got there first. HMR patches pass
     * no `dedup` so they always re-run the factory and replace the registered
     * exports.
     *
     * @type {<T>(id: string, fn: any, dedup: any, res: T) => () => T}
     * @internal
     */
    createEsmInitializer: <T>(id: string, fn: any, dedup: any, res: T) => () => T;
    /**
     * __commonJSMin
     *
     * Same dedup gate as createEsmInitializer. With `dedup` truthy and `id`
     * registered, reuse the registered exports object; otherwise run the
     * factory.
     *
     * @type {<T extends { exports: any }>(id: string, cb: any, dedup: any, mod: { exports: any }, registered: any) => () => T}
     * @internal
     */
    createCjsInitializer: <T extends {
        exports: any;
    }>(id: string, cb: any, dedup: any, mod: {
        exports: any;
    }, registered: any) => () => T;
    /** @internal */
    __toESM: any;
    /** @internal */
    __toCommonJS: any;
    /** @internal */
    __exportAll: any;
    /**
     * @param {boolean} [isNodeMode]
     * @returns {(mod: any) => any}
     * @internal
     */
    __toDynamicImportESM: (isNodeMode?: boolean) => (mod: any) => any;
    /** @internal */
    __reExport: any;
    sendModuleRegisteredMessage: (module: string) => void;
}
export type DevRuntimeMessage = {
    type: "hmr:module-registered";
    modules: string[];
};
export type Messenger = {
    send(message: DevRuntimeMessage): void;
};
declare class Module {
    /**
     * @param {string} id
     */
    constructor(id: string);
    /**
     * @type {{ exports: any }}
     */
    exportsHolder: {
        exports: any;
    };
    /**
     * @type {string}
     */
    id: string;
    get exports(): any;
}
export {};
