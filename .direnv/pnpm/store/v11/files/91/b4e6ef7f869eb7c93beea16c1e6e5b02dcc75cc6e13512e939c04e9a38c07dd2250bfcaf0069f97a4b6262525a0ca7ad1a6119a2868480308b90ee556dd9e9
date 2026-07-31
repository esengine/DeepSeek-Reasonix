# Cytoscape.js AGENTS.md

Guidelines for agents contributing to the Cytoscape.js graph theory and visualisation library.

## Environment & tooling
- Use Node via `.nvmrc` when possible: `nvm use` or `mise en`.
- Use `npm`; the repo is configured around `package-lock.json` and the existing npm scripts.
- Library source of truth is in `src/`. Documentation source lives in `documentation/md/`, `documentation/demos/`, and `documentation/docmaker.json`. Built artifacts in `build/`, `dist/`, and generated files under `documentation/` should only be updated via the project scripts.
- Bundles are produced with Rollup from `src/index.mjs` into UMD, minified UMD, CJS, ESM, and minified ESM outputs.
- The repo uses ESM source files (`.mjs`), ESLint, Mocha, and Playwright.
- Before starting significant work, read any repo docs directly related to the area you are changing. For architecture context, start with `documentation/md/architecture.md`.
- If you want to read the documentation, you can grep `documentation/docmaker.json`, which contains all the documentation data (and API in JSON format).  You can search for things like "cy.on" for the `cy.on()` method.  `docmaker.json` references markdown files in `documentation/md/` for prose that elaborates on particular API methods and also for general prose sections, like the intro or getting started sections.  You can also grep `documentation/md/**/*.md` generally for doc searches.  The paths broadly match the `src/**/*.mjs` paths.

## Development flow
- Make sure dependencies are installed when you first start: `npm install`.
- Install Playwright browsers before running browser coverage or the full test suite on a fresh environment: `npx playwright install --with-deps`.
- Make your changes.
- Lint source files: `npm run lint`.
- Run the narrowest useful test loop while iterating, but run the relevant verification before handing work back:
  - Source or algorithm changes: `npm run test:js` and `npm run test:modules`.
  - Renderer or interaction changes: `npm run test:js`, `npm run test:modules`, and sanity check in `debug/` via `npm run watch`; run Playwright when browser behaviour is affected.
  - Bundle, packaging, or docs pipeline changes: `npm run build`, `npm run docs`, and any targeted release script checks that apply.
  - If the change is broad or you are unsure, run `npm test`.
- Build all bundles but only if you're modifying the build system: `npm run build`.

## Repository structure
- `src/`: Main library source.
  - `src/core/`: Core instance lifecycle, viewport, rendering, style, layout, animation, and notifications.
  - `src/collection/`: Collection APIs, traversals, dimensions, styling, and graph algorithms.
  - `src/style/`: Style parsing, application, bypasses, and stylesheet helpers.
  - `src/selector/`: Selector parsing and matching.
  - `src/extensions/`: Built-in layouts and renderers.
    - `src/extensions/renderer/base/`: Shared renderer state and geometry logic.
    - `src/extensions/renderer/canvas/`: Canvas renderer, drawing pipeline, caches, and WebGL helpers.
    - `src/extensions/layout/`: Built-in layouts like grid, cose, concentric, and breadthfirst.
  - `src/util/`: Shared low-level helpers.
- `test/`: Mocha tests. Add regression coverage here for API and logic changes.
- `debug/`: Manual visual and interaction test pages. Use this for renderer, interaction, and gesture changes that are hard to verify in unit tests alone.
- `playwright-tests/` and `playwright.config.js`: Browser-level regression coverage.
- `documentation/`: Generated site plus source markdown, demos, and the doc generator.
  - `documentation/md/`: Documentation source.
  - `documentation/demos/`: Demo apps and assets used by the docs site.
  - `documentation/docmaker.mjs`: Docs build entrypoint.
- `.github/workflows/`: CI and release workflows.
- `benchmark/`: Performance comparisons and targeted benchmark runners.
- `typescript/`: TypeScript-related tests and fixtures.

## Code standards
1. Preserve the existing style: two-space indentation, single quotes, ESM imports/exports, and concise readable functions.
2. Do not hand-edit generated outputs when a source file exists instead. In particular, prefer editing `src/` and `documentation/md/` over generated files in `build/`, `dist/`, and compiled docs assets.
3. Keep module boundaries aligned with the existing architecture. New source files should live near the corresponding subsystem in `src/`.
4. When fixing a bug, add or update a regression test whenever practical. Put public-behaviour tests in `test/`; keep internal-only coverage in `test/modules/` when applicable.
5. For renderer, gesture, or grab-state changes, verify behaviour in `debug/` because visual regressions are not always caught by Mocha alone.  You need to control a browser instance to use this and you need to run `npm run watch` to run a dev server with auto-rebuild.
6. Keep docs in sync with API or behaviour changes. Update `documentation/md/`, demos, and `docmaker.json` inputs rather than patching generated HTML by hand.
7. Avoid introducing new build tools, frameworks, or repo-wide conventions unless the task explicitly requires it.
8. When adding new top-level workflows, major directories, or important source areas not already documented here, update `AGENTS.md`.

## Testing notes
- `npm test` matches CI closely: GitHub Actions installs dependencies, installs Playwright browsers, and runs `npm test`.
- `npm run test:build` exercises the built bundle rather than source files; use it when a bug could be introduced by bundling or build-time transforms.
- Playwright setup depends on a built UMD bundle and a local HTTP server. Use the existing scripts rather than inventing a parallel harness.

## Documentation notes
- Documentation HTML is generated. Do not edit generated docs directly when the corresponding markdown or template source (i.e. `docmaker.json` and `template.html`) should be changed instead.

## Contribution notes
- Keep changes narrowly scoped. Cytoscape.js has a large public API and small internal regressions can surface broadly.
- Prefer extending existing tests, demos, and docs over adding parallel mechanisms.
- If a change affects public API semantics, selectors, style behaviour, layouts, rendering, or documentation structure, call that out explicitly in your summary to the user.
