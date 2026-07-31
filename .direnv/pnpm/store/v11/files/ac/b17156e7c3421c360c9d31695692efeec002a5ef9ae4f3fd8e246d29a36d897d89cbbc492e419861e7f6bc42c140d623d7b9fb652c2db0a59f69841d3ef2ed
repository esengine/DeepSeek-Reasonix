import { HotPayload } from "#types/hmrPayload";

//#region src/shared/invokeMethods.d.ts
interface FetchFunctionOptions {
  cached?: boolean;
  startOffset?: number;
}
type FetchResult = CachedFetchResult | ExternalFetchResult | ViteFetchResult;
interface CachedFetchResult {
  /**
  * If the module is cached in the runner, this confirms
  * it was not invalidated on the server side.
  */
  cache: true;
}
interface ExternalFetchResult {
  /**
  * The path to the externalized module starting with file://.
  * By default this will be imported via a dynamic "import"
  * instead of being transformed by Vite and loaded with the Vite runner.
  */
  externalize: string;
  /**
  * Type of the module. Used to determine if the import statement is correct.
  * For example, if Vite needs to throw an error if a variable is not actually exported.
  */
  type: "module" | "commonjs" | "builtin" | "network";
}
interface ViteFetchResult {
  /**
  * Code that will be evaluated by the Vite runner.
  * By default this will be wrapped in an async function.
  */
  code: string;
  /**
  * File path of the module on disk.
  * This will be resolved as import.meta.url/filename.
  * Will be `null` for virtual modules.
  */
  file: string | null;
  /**
  * Module ID in the server module graph.
  */
  id: string;
  /**
  * Module URL used in the import.
  */
  url: string;
  /**
  * Invalidate module on the client side.
  */
  invalidate: boolean;
}
type InvokeMethods = {
  fetchModule: (id: string, importer?: string, options?: FetchFunctionOptions) => Promise<FetchResult>;
  getBuiltins: () => Promise<Array<{
    type: "string";
    value: string;
  } | {
    type: "RegExp";
    source: string;
    flags: string;
  }>>;
};
//#endregion
//#region src/shared/moduleRunnerTransport.d.ts
type ModuleRunnerTransportHandlers = {
  onMessage: (data: HotPayload) => void;
  onDisconnection: () => void;
};
/**
* "send and connect" or "invoke" must be implemented
*/
interface ModuleRunnerTransport {
  connect?(handlers: ModuleRunnerTransportHandlers): Promise<void> | void;
  disconnect?(): Promise<void> | void;
  send?(data: HotPayload): Promise<void> | void;
  invoke?(data: HotPayload): Promise<{
    result: any;
  } | {
    error: any;
  }>;
  timeout?: number;
}
interface NormalizedModuleRunnerTransport {
  connect?(onMessage?: (data: HotPayload) => void): Promise<void> | void;
  disconnect?(): Promise<void> | void;
  send(data: HotPayload): Promise<void>;
  invoke<T extends keyof InvokeMethods>(name: T, data: Parameters<InvokeMethods[T]>): Promise<ReturnType<Awaited<InvokeMethods[T]>>>;
}
declare const createWebSocketModuleRunnerTransport: (options: {
  createConnection: () => WebSocket;
  pingInterval?: number;
}) => Required<Pick<ModuleRunnerTransport, "connect" | "disconnect" | "send">>;
//#endregion
export { ExternalFetchResult as a, ViteFetchResult as c, createWebSocketModuleRunnerTransport as i, ModuleRunnerTransportHandlers as n, FetchFunctionOptions as o, NormalizedModuleRunnerTransport as r, FetchResult as s, ModuleRunnerTransport as t };