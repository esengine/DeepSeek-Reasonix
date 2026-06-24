import { defineConfig } from 'astro/config';
import sitemap from '@astrojs/sitemap';

// Served from the custom domain qiaotongagent.io at the site root.
export default defineConfig({
  site: 'https://qiaotongagent.io',
  build: { assets: 'static' },
  integrations: [sitemap()],
});
