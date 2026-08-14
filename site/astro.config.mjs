import { defineConfig } from 'astro/config';
import sitemap from '@astrojs/sitemap';

// intelifar enterprise IP workspace delivery target.
export default defineConfig({
  site: 'https://intelifar.cn',
  // Documentation code samples intentionally use source newlines as content.
  // Astro 7's JSX-style compression removes those newlines unless disabled.
  compressHTML: false,
  build: { assets: 'static' },
  // Public activation/share scripts must remain external because the gateway's
  // strict CSP intentionally forbids both inline scripts and data: script URLs.
  vite: { build: { assetsInlineLimit: 0 } },
  integrations: [sitemap({
    filter: (page) => !/\/changelog\/(?:stable|preview)\/?$/.test(page) &&
      !/\/changelog\/v\d+\.\d+\.\d+-/.test(page),
  })],
});
