# @vitejs/plugin-react [![npm](https://img.shields.io/npm/v/@vitejs/plugin-react.svg)](https://npmjs.com/package/@vitejs/plugin-react)

The default Vite plugin for React projects.

- enable [Fast Refresh](https://www.npmjs.com/package/react-refresh) in development (requires react >= 16.9)
- use the [automatic JSX runtime](https://legacy.reactjs.org/blog/2020/09/22/introducing-the-new-jsx-transform.html)
- small installation size

```js
// vite.config.js
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
})
```

## Options

### include

Includes `.js`, `.jsx`, `.ts` & `.tsx` by default. This option can be used to add fast refresh to `.mdx` files:

```js
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import mdx from '@mdx-js/rollup'

export default defineConfig({
  plugins: [
    { enforce: 'pre', ...mdx() },
    react({ include: /\.(mdx|js|jsx|ts|tsx)$/ }),
  ],
})
```

### exclude

The default value is `/node_modules/`. You may use it to exclude JSX/TSX files that runs in a worker or are not React files.
Except if explicitly desired, you should keep `node_modules` in the exclude list:

```js
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [
    react({ exclude: [/\/pdf\//, /\.solid\.tsx$/, /\/node_modules\//] }),
  ],
})
```

### jsxImportSource

Control where the JSX factory is imported from. By default, this is inferred from `jsxImportSource` from corresponding a tsconfig file for a transformed file.

```js
react({ jsxImportSource: '@emotion/react' })
```

### jsxRuntime

By default, the plugin uses the [automatic JSX runtime](https://legacy.reactjs.org/blog/2020/09/22/introducing-the-new-jsx-transform.html). However, if you encounter any issues, you may opt out using the `jsxRuntime` option.

```js
react({ jsxRuntime: 'classic' })
```

### reactRefreshHost

The `reactRefreshHost` option is only necessary in a module federation context. It enables HMR to work between a remote & host application. In your remote Vite config, you would add your host origin:

```js
react({ reactRefreshHost: 'http://localhost:3000' })
```

Under the hood, this simply updates the React Fash Refresh runtime URL from `/@react-refresh` to `http://localhost:3000/@react-refresh` to ensure there is only one Refresh runtime across the whole application. Note that if you define `base` option in the host application, you need to include it in the option, like: `http://localhost:3000/{base}`.

## React Compiler

[React Compiler](https://react.dev/learn/react-compiler) support is available via the exported `reactCompilerPreset` helper, which requires [`@rolldown/plugin-babel`](https://npmx.dev/package/@rolldown/plugin-babel) and [`babel-plugin-react-compiler`](https://npmx.dev/package/babel-plugin-react-compiler) and [`@babel/core`](https://npmx.dev/package/@babel/core) as peer dependencies:

```sh
npm install -D @rolldown/plugin-babel @babel/core babel-plugin-react-compiler
```

If you are using TypeScript, you will also need to install [`@types/babel__core`](https://npmx.dev/package/@types/babel__core):

```sh
npm install -D @types/babel__core
```

```js
// vite.config.js
import { defineConfig } from 'vite'
import react, { reactCompilerPreset } from '@vitejs/plugin-react'
import babel from '@rolldown/plugin-babel'

export default defineConfig({
  plugins: [react(), babel({ presets: [reactCompilerPreset()] })],
})
```

The `reactCompilerPreset` accepts an optional options object with the following properties:

- `compilationMode` — Set to `'annotation'` to only compile components annotated with `"use memo"`.
- `target` — Set to `'17'` or `'18'` to target older React versions (uses `react-compiler-runtime` instead of `react/compiler-runtime`).

Additional options can be found in the [documentation](https://react.dev/reference/react-compiler/configuration).

```js
babel({
  presets: [reactCompilerPreset({ compilationMode: 'annotation' })],
})
```

> [!TIP]
>
> `reactCompilerPreset` is only a convenient helper with a preconfigured filter. You can configure override the filters to fit your project structure or code. For example, if you know a large portion of your files are never React/hook-related or won't benefit from the React Compiler, you can aggressively exclude them via `rolldown.filter`:
>
> ```js
> const myPreset = reactCompilerPreset()
> myPreset.rolldown.filter.id.exclude = ['src/legacy/**', 'src/utils/**']
>
> babel({
>   presets: [myPreset],
> })
> ```

## `@vitejs/plugin-react/preamble`

The package provides `@vitejs/plugin-react/preamble` to initialize HMR runtime from client entrypoint for SSR applications which don't use [`transformIndexHtml` API](https://vite.dev/guide/api-javascript.html#vitedevserver). For example:

```js
// [entry.client.js]
import '@vitejs/plugin-react/preamble'
```

Alternatively, you can manually call `transformIndexHtml` during SSR, which sets up equivalent initialization code. Here's an example for an Express server:

```js
app.get('/', async (req, res, next) => {
  try {
    let html = fs.readFileSync(path.resolve(root, 'index.html'), 'utf-8')

    // Transform HTML using Vite plugins.
    html = await viteServer.transformIndexHtml(req.url, html)

    res.send(html)
  } catch (e) {
    return next(e)
  }
})
```

Otherwise, you'll get the following error:

```
Uncaught Error: @vitejs/plugin-react can't detect preamble. Something is wrong.
```

## Consistent components exports

For React refresh to work correctly, your file should only export React components. You can find a good explanation in the [Gatsby docs](https://www.gatsbyjs.com/docs/reference/local-development/fast-refresh/#how-it-works).

If an incompatible change in exports is found, the module will be invalidated and HMR will propagate. To make it easier to export simple constants alongside your component, the module is only invalidated when their value changes.

You can catch mistakes and get more detailed warnings with this [ESLint rule](https://github.com/ArnaudBarre/eslint-plugin-react-refresh), or the equivalent [Oxlint rule](https://oxc.rs/docs/guide/usage/linter/rules/react/only-export-components.html).
