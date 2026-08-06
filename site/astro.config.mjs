import { defineConfig } from 'astro/config';
import sitemap from '@astrojs/sitemap';

// Served from the custom domain reasonix.io at the site root.
export default defineConfig({
  site: 'https://reasonix.io',
  // Documentation code samples intentionally use source newlines as content.
  // Astro 7's JSX-style compression removes those newlines unless disabled.
  compressHTML: false,
  build: { assets: 'static' },
  integrations: [sitemap({
    filter: (page) => !/\/changelog\/(?:stable|preview)\/?$/.test(page) &&
      !/\/changelog\/v\d+\.\d+\.\d+-/.test(page),
  })],
});
