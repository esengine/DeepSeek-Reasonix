import { defineConfig } from 'astro/config';

// `site` is the canonical origin for OG / canonical tags.
// `base: './'` emits **relative** asset paths so the same build works at
//   - https://esengine.github.io/DeepSeek-Reasonix/   (GitHub Pages project URL)
//   - http://reasonix.io/                             (custom domain, served from root)
// The previous `base: '/DeepSeek-Reasonix'` baked that prefix into every
// emitted URL, so the custom domain rendered unstyled (CSS 404'd).
// Relative URLs are deploy-anywhere and add zero cost on the github.io path.
export default defineConfig({
  site: 'https://esengine.github.io',
  base: './',
  build: { assets: 'static' },
});
