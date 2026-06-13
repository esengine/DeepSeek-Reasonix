import { defineConfig } from 'astro/config';
import sitemap from '@astrojs/sitemap';

// Served from the custom domain reasonix.io at the site root.
export default defineConfig({
  site: 'https://reasonix.io',
  build: { assets: 'static' },
  integrations: [sitemap()],
});
