import { readConfig, webSearchEndpoint, webSearchEngine, writeConfig } from "../../../../config.js";
import type { SlashHandler } from "../dispatch.js";

export const handlers: Record<string, SlashHandler> = {
  "search-engine": (args, _loop, ctx) => {
    const engine = args[0];
    if (!engine || (engine !== "mojeek" && engine !== "searxng")) {
      return {
        info: [
          `Current web search engine: ${webSearchEngine()}`,
          `SearXNG endpoint: ${webSearchEndpoint()}`,
          "",
          "Usage:",
          "  /search-engine mojeek            use Mojeek (default, no external deps)",
          "  /search-engine searxng            use SearXNG at default endpoint",
          "  /search-engine searxng <url>      use SearXNG at custom endpoint",
          "",
          "Alias: /se",
          "",
          "SearXNG is a self-hosted metasearch engine (https://github.com/searxng/searxng).",
          "Install it with:  docker run -d -p 8080:8080 searxng/searxng",
        ].join("\n"),
      };
    }

    const cfg = readConfig();
    cfg.webSearchEngine = engine;
    if (engine === "searxng" && args[1]) {
      const raw = args[1];
      cfg.webSearchEndpoint = raw.includes("://") ? raw : `http://${raw}`;
    }
    writeConfig(cfg);

    ctx.postInfo?.(
      `Switched web search engine to "${engine}". ${engine === "searxng" ? `Make sure SearXNG is running at ${webSearchEndpoint()}.` : ""}`,
    );

    return {
      info: `✓ Web search engine set to "${engine}"${engine === "searxng" ? ` (${webSearchEndpoint()})` : ""}. Next assistant turn will pick up the change.`,
    };
  },
  se: (args, loop, ctx) => handlers["search-engine"]!(args, loop, ctx),
};
