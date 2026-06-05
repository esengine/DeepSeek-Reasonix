import { defineConfig } from 'astro/config';

// Served from GitHub Pages under the repo subpath.
export default defineConfig({
  site: 'https://esengine.github.io',
  base: '/DeepSeek-Reasonix',
  build: { assets: 'static' },
});
