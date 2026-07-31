import { Plugin } from "vite";
import { ReactCompilerBabelPluginOptions, RolldownBabelPreset } from "#optionalTypes";

//#region src/reactCompilerPreset.d.ts
declare const reactCompilerPreset: (options?: ReactCompilerBabelPluginOptions) => RolldownBabelPreset;
//#endregion
//#region src/index.d.ts
interface Options {
  /**
   * Can be used to process extra files like `.mdx`
   * @example include: /\.(mdx|js|jsx|ts|tsx)$/
   * @default /\.[tj]sx?$/
   */
  include?: string | RegExp | Array<string | RegExp>;
  /**
   * Can be used to exclude JSX/TSX files that runs in a worker or are not React files.
   * Except if explicitly desired, you should keep node_modules in the exclude list
   * @example exclude: [/\/pdf\//, /\.solid\.tsx$/, /\/node_modules\//]
   * @default /\/node_modules\//
   */
  exclude?: string | RegExp | Array<string | RegExp>;
  /**
   * Control where the JSX factory is imported from.
   * https://oxc.rs/docs/guide/usage/transformer/jsx.html#import-source
   * @default 'react'
   */
  jsxImportSource?: string;
  /**
   * Note: Skipping React import with classic runtime is not supported from v4
   * @default "automatic"
   */
  jsxRuntime?: 'classic' | 'automatic';
  /**
   * React Fast Refresh runtime URL prefix.
   * Useful in a module federation context to enable HMR by specifying
   * the host application URL in the Vite config of a remote application.
   * @example
   * reactRefreshHost: 'http://localhost:3000'
   */
  reactRefreshHost?: string;
}
declare function viteReact(opts?: Options): Plugin[];
declare namespace viteReact {
  var preambleCode: string;
}
declare function viteReactForCjs(this: unknown, options: Options): Plugin[];
//#endregion
export { Options, viteReact as default, viteReactForCjs as "module.exports", reactCompilerPreset };