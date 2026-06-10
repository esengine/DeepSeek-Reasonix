import { defineConfig } from 'astro/config';

// Served from the custom domain reasonix.io at the site root.
export default defineConfig({
  site: 'https://reasonix.io',
  build: { assets: 'static' },
});
