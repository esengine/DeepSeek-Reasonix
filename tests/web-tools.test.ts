import { lookup } from "node:dns/promises";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { loadMetasoApiKey, writeConfig } from "../src/config.js";
import { ToolRegistry } from "../src/tools.js";
import {
  formatSearchResults,
  htmlToText,
  parseBingResults,
  parseSearxngHtmlResults,
  registerWebTools,
  webFetch,
  webSearch,
} from "../src/tools/web.js";

vi.mock("node:dns/promises", () => ({
  lookup: vi.fn(async () => [{ address: "93.184.216.34", family: 4 }]),
}));

describe("htmlToText", () => {
  it("strips script/style/nav/footer and preserves paragraph breaks", () => {
    const html = `
      <html><head><title>x</title><style>body{color:red}</style></head>
      <body>
        <nav><a>skip</a></nav>
        <p>Hello <strong>world</strong>.</p>
        <p>Second paragraph.</p>
        <script>evil()</script>
        <footer>fine print</footer>
      </body></html>
    `;
    const out = htmlToText(html);
    expect(out).toContain("Hello world.");
    expect(out).toContain("Second paragraph.");
    expect(out).not.toContain("evil");
    expect(out).not.toContain("skip");
    expect(out).not.toContain("fine print");
    expect(out).not.toContain("color:red");
    expect(out).toMatch(/Hello world\.\n\nSecond paragraph\./);
  });

  it("decodes the common entities", () => {
    expect(htmlToText("<p>a &amp; b &lt;c&gt; &quot;d&quot;</p>")).toContain('a & b <c> "d"');
  });

  it("collapses whitespace runs but keeps paragraph breaks", () => {
    const out = htmlToText("<p>one    two</p><p>three</p>");
    expect(out).toBe("one two\n\nthree");
  });
});

describe("parseBingResults", () => {
  // Fixture mirrors the shape cn.bing.com returns as of 2026-05 (captured live).
  // Each result is a `<li class="b_algo">` with `<h2><a href>` + `<div class="b_caption"><p>`.
  const sampleHtml = `
    <ol id="b_results">
      <li class="b_algo" data-id iid=SERP.5339>
        <h2 class="">
          <a target="_blank" href="https://example.com/a" h="ID=SERP,5145.2">
            <strong>Flutter</strong> 3.19 release notes
          </a>
        </h2>
        <div class="b_caption">
          <p class="b_lineclamp2">Flutter 3.19 introduces <strong>new Navigator</strong>&nbsp;APIs &amp; more.</p>
        </div>
      </li>
      <li class="b_algo" data-id iid=SERP.5340>
        <h2><a target="_blank" href="https://medium.com/flutter/x">What's new in 3.19</a></h2>
        <div class="b_caption"><p>An overview post.</p></div>
      </li>
    </ol>
  `;

  it("extracts title/url/snippet from b_algo blocks", () => {
    const items = parseBingResults(sampleHtml);
    expect(items).toHaveLength(2);
    expect(items[0]).toEqual({
      title: "Flutter 3.19 release notes",
      url: "https://example.com/a",
      snippet: "Flutter 3.19 introduces new Navigator APIs & more.",
    });
    expect(items[1]).toEqual({
      title: "What's new in 3.19",
      url: "https://medium.com/flutter/x",
      snippet: "An overview post.",
    });
  });

  it("returns empty on markup that doesn't match the expected shape", () => {
    expect(parseBingResults("<html><body>nothing here</body></html>")).toEqual([]);
  });

  it("handles a b_algo block with no b_caption sibling (empty snippet)", () => {
    const html = `
      <li class="b_algo">
        <h2><a href="https://example.com/s">Solo</a></h2>
      </li>
    `;
    const items = parseBingResults(html);
    expect(items).toHaveLength(1);
    expect(items[0]).toEqual({
      title: "Solo",
      url: "https://example.com/s",
      snippet: "",
    });
  });

  it("skips a b_algo block missing the h2 anchor", () => {
    const html = `
      <li class="b_algo">
        <div class="b_caption"><p>orphan snippet</p></div>
      </li>
    `;
    expect(parseBingResults(html)).toEqual([]);
  });
});

describe("parseSearxngHtmlResults", () => {
  const sampleHtml = `
    <article class="result">
      <h3><a href="https://example.com/rust">Rust programming language</a></h3>
      <p>A systems language focused on safety and concurrency.</p>
    </article>
    <article class="result">
      <h3><a href="https://example.com/go">Go by example</a></h3>
      <p>Learn Go with annotated example programs.</p>
    </article>
  `;

  it("extracts title/url/snippet from article.result markup", () => {
    const items = parseSearxngHtmlResults(sampleHtml);
    expect(items).toHaveLength(2);
    expect(items[0]).toEqual({
      title: "Rust programming language",
      url: "https://example.com/rust",
      snippet: "A systems language focused on safety and concurrency.",
    });
    expect(items[1]).toEqual({
      title: "Go by example",
      url: "https://example.com/go",
      snippet: "Learn Go with annotated example programs.",
    });
  });

  it("falls back to h3 a[href] when no article.result exists", () => {
    const html = `
      <div>
        <h3><a href="https://example.com/x">Title X</a></h3>
        <p>Snippet for X.</p>
      </div>
    `;
    const items = parseSearxngHtmlResults(html);
    expect(items).toHaveLength(1);
    expect(items[0]).toEqual({
      title: "Title X",
      url: "https://example.com/x",
      snippet: "Snippet for X.",
    });
  });

  it("returns empty on markup with no recognizable results", () => {
    expect(parseSearxngHtmlResults("<html><body>nothing here</body></html>")).toEqual([]);
  });
});

describe("webSearch", () => {
  const twoResultsHtml = `
    <li class="b_algo"><h2><a href="https://example.com/a">A</a></h2>
      <div class="b_caption"><p>snippet A</p></div></li>
    <li class="b_algo"><h2><a href="https://example.com/b">B</a></h2>
      <div class="b_caption"><p>snippet B</p></div></li>`;

  it("GETs cn.bing.com with a browser UA and query string", async () => {
    const captured: { url: string; method: string; ua: string } = { url: "", method: "", ua: "" };
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(async (url: string | URL, init?: RequestInit) => {
      captured.url = String(url);
      captured.method = init?.method ?? "GET";
      const headers = (init?.headers ?? {}) as Record<string, string>;
      captured.ua = headers["User-Agent"] ?? "";
      return new Response(twoResultsHtml, {
        status: 200,
        headers: { "Content-Type": "text/html" },
      });
    }) as unknown as typeof fetch;
    try {
      const out = await webSearch("flutter 3.19", { topK: 2 });
      expect(captured.url).toContain("cn.bing.com/search");
      expect(captured.url).toContain("q=flutter%203.19");
      expect(captured.method).toBe("GET");
      expect(captured.ua).toMatch(/Mozilla\/5.0/);
      expect(out).toHaveLength(2);
      expect(out[0]?.title).toBe("A");
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  it("clamps topK to [1, 10]", async () => {
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(
      async () => new Response(twoResultsHtml, { status: 200 }),
    ) as unknown as typeof fetch;
    try {
      const outMax = await webSearch("x", { topK: 99 });
      expect(outMax.length).toBeLessThanOrEqual(10);
      const outMin = await webSearch("x", { topK: 0 });
      expect(outMin.length).toBe(1);
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  it("throws on non-2xx", async () => {
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(
      async () => new Response("blocked", { status: 429 }),
    ) as unknown as typeof fetch;
    try {
      await expect(webSearch("q")).rejects.toThrow(/web_search 429/);
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  it("annotates 429 with a wait-and-retry hint", async () => {
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(
      async () => new Response("blocked", { status: 429 }),
    ) as unknown as typeof fetch;
    try {
      await expect(webSearch("q")).rejects.toThrow(/web_search 429.*try:.*wait/);
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  it("annotates 403 with a hint that the backend is blocking", async () => {
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(
      async () => new Response("forbidden", { status: 403 }),
    ) as unknown as typeof fetch;
    try {
      await expect(webSearch("q")).rejects.toThrow(/web_search 403.*try:/);
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  it("annotates 5xx with a transient-retry hint", async () => {
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(
      async () => new Response("broken", { status: 503 }),
    ) as unknown as typeof fetch;
    try {
      await expect(webSearch("q")).rejects.toThrow(/web_search 503.*try:.*retry in 30s/);
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  it("returns [] on a legitimately empty 'No results' page", async () => {
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(
      async () =>
        new Response("<html><body>Your search did not match any documents.</body></html>", {
          status: 200,
        }),
    ) as unknown as typeof fetch;
    try {
      const out = await webSearch("zzyzx nothing here");
      expect(out).toEqual([]);
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  it("surfaces a clear error on an anti-bot interstitial", async () => {
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(
      async () =>
        new Response("<html>Please solve the captcha to continue.</html>", { status: 200 }),
    ) as unknown as typeof fetch;
    try {
      await expect(webSearch("q")).rejects.toThrow(/anti-bot|rate-limited|blocked/);
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  it("surfaces a diagnostic error when the response looks like neither empty-nor-results", async () => {
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(
      async () =>
        new Response("<html><body>totally unexpected shape</body></html>", { status: 200 }),
    ) as unknown as typeof fetch;
    try {
      await expect(webSearch("q")).rejects.toThrow(/doesn't look like a real empty page/);
    } finally {
      globalThis.fetch = originalFetch;
    }
  });
});

describe("searchMetaso", () => {
  const originalMetasoKey = process.env.METASO_API_KEY;
  const sampleResponse = {
    credits: 3,
    total: 72,
    webpages: [
      {
        title: "Result One",
        link: "https://example.com/1",
        snippet: "Snippet one.",
        score: "high",
        position: 1,
      },
      {
        title: "Result Two",
        link: "https://example.com/2",
        summary: "Summary two.",
        score: "medium",
        position: 2,
      },
    ],
  };

  beforeEach(() => {
    process.env.METASO_API_KEY = "test-metaso-key";
  });

  afterEach(() => {
    if (originalMetasoKey === undefined) {
      // biome-ignore lint/performance/noDelete: env var must be restored to absent
      delete process.env.METASO_API_KEY;
    } else {
      process.env.METASO_API_KEY = originalMetasoKey;
    }
  });

  it("requires an API key before contacting Metaso", async () => {
    // biome-ignore lint/performance/noDelete: env var must be absent, not "undefined"
    delete process.env.METASO_API_KEY;
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn() as unknown as typeof fetch;
    try {
      await expect(webSearch("q", { engine: "metaso" })).rejects.toThrow(/Metaso.*API key/i);
      expect(globalThis.fetch).not.toHaveBeenCalled();
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  it("POSTs to metaso API with JSON body and auth header", async () => {
    const captured: { url: string; method: string; headers: Record<string, string>; body: string } =
      { url: "", method: "", headers: {}, body: "" };
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(async (url: string | URL, init?: RequestInit) => {
      captured.url = String(url);
      captured.method = init?.method ?? "GET";
      captured.headers = (init?.headers ?? {}) as Record<string, string>;
      captured.body = String(init?.body ?? "");
      return new Response(JSON.stringify(sampleResponse), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }) as unknown as typeof fetch;
    try {
      const out = await webSearch("test query", { engine: "metaso", topK: 5 });
      expect(captured.url).toContain("metaso.cn/api/v1/search");
      expect(captured.method).toBe("POST");
      expect(captured.headers.Authorization).toBe(`Bearer ${loadMetasoApiKey()}`);
      expect(captured.headers["Content-Type"]).toBe("application/json");
      const body = JSON.parse(captured.body);
      expect(body.q).toBe("test query");
      expect(body.scope).toBe("webpage");
      expect(body.size).toBe(5);
      expect(out).toHaveLength(2);
      expect(out[0]).toEqual({
        title: "Result One",
        url: "https://example.com/1",
        snippet: "Snippet one.",
      });
      expect(out[1]).toEqual({
        title: "Result Two",
        url: "https://example.com/2",
        snippet: "Summary two.",
      });
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  it("throws daily limit error on code 3003", async () => {
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(
      async () =>
        new Response(JSON.stringify({ code: 3003, message: "今日调用次数已达上限" }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
    ) as unknown as typeof fetch;
    try {
      await expect(webSearch("q", { engine: "metaso" })).rejects.toThrow(/daily search limit/);
      await expect(webSearch("q", { engine: "metaso" })).rejects.toThrow(
        /metaso.cn\/search-api\/playground/,
      );
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  it("throws unauthorized error on code 2005 (invalid API key)", async () => {
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(
      async () =>
        new Response(JSON.stringify({ code: 2005, message: "API密钥无效" }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
    ) as unknown as typeof fetch;
    try {
      await expect(webSearch("q", { engine: "metaso" })).rejects.toThrow(/API key rejected/);
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  it("throws rate limit error on 429", async () => {
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(
      async () =>
        new Response(JSON.stringify({ message: "rate limited" }), {
          status: 429,
          headers: { "Content-Type": "application/json" },
        }),
    ) as unknown as typeof fetch;
    try {
      await expect(webSearch("q", { engine: "metaso" })).rejects.toThrow(/rate-limited/);
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  it("returns empty array on 0 results", async () => {
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(
      async () =>
        new Response(JSON.stringify({ credits: 1, total: 0 }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
    ) as unknown as typeof fetch;
    try {
      const out = await webSearch("q", { engine: "metaso" });
      expect(out).toEqual([]);
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  it("clamps topK to [1, 100]", async () => {
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(
      async () =>
        new Response(JSON.stringify(sampleResponse), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
    ) as unknown as typeof fetch;
    try {
      const outMax = await webSearch("x", { engine: "metaso", topK: 999 });
      expect(outMax.length).toBeLessThanOrEqual(2);
      const outMin = await webSearch("x", { engine: "metaso", topK: 0 });
      expect(outMin.length).toBe(1);
    } finally {
      globalThis.fetch = originalFetch;
    }
  });
});

describe("searchTavily", () => {
  const sampleResponse = {
    query: "test query",
    results: [
      {
        title: "First Hit",
        url: "https://example.com/1",
        content: "First snippet.",
        score: 0.93,
      },
      {
        title: "Second Hit",
        url: "https://example.com/2",
        content: "Second snippet.",
        score: 0.81,
      },
    ],
    response_time: 0.42,
  };

  it("requires an API key — throws a setup-pointing error when none is set", async () => {
    const origKey = process.env.TAVILY_API_KEY;
    // biome-ignore lint/performance/noDelete: env var must be absent, not "undefined"
    delete process.env.TAVILY_API_KEY;
    try {
      await expect(webSearch("q", { engine: "tavily" })).rejects.toThrow(/Tavily.*API key/i);
      // Plain-string match — checking the error message includes the signup URL,
      // not testing URL safety; using a regex here trips CodeQL's missing-anchor rule.
      await expect(webSearch("q", { engine: "tavily" })).rejects.toThrow("tavily.com");
    } finally {
      if (origKey !== undefined) process.env.TAVILY_API_KEY = origKey;
    }
  });

  it("POSTs to Tavily with the api_key in the body", async () => {
    const origKey = process.env.TAVILY_API_KEY;
    process.env.TAVILY_API_KEY = "tvly-test-key";
    const captured: { url: string; method: string; body: string } = {
      url: "",
      method: "",
      body: "",
    };
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(async (url: string | URL, init?: RequestInit) => {
      captured.url = String(url);
      captured.method = init?.method ?? "GET";
      captured.body = String(init?.body ?? "");
      return new Response(JSON.stringify(sampleResponse), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }) as unknown as typeof fetch;
    try {
      const out = await webSearch("test query", { engine: "tavily", topK: 5 });
      expect(captured.url).toBe("https://api.tavily.com/search");
      expect(captured.method).toBe("POST");
      const body = JSON.parse(captured.body);
      expect(body.api_key).toBe("tvly-test-key");
      expect(body.query).toBe("test query");
      expect(body.max_results).toBe(5);
      expect(out).toHaveLength(2);
      expect(out[0]).toEqual({
        title: "First Hit",
        url: "https://example.com/1",
        snippet: "First snippet.",
      });
    } finally {
      globalThis.fetch = originalFetch;
      if (origKey === undefined) {
        // biome-ignore lint/performance/noDelete: same reason — must drop, not set to "undefined"
        delete process.env.TAVILY_API_KEY;
      } else {
        process.env.TAVILY_API_KEY = origKey;
      }
    }
  });

  it("maps 401/403 to a key-rejected error", async () => {
    const origKey = process.env.TAVILY_API_KEY;
    process.env.TAVILY_API_KEY = "tvly-bad";
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(
      async () => new Response("forbidden", { status: 403 }),
    ) as unknown as typeof fetch;
    try {
      await expect(webSearch("q", { engine: "tavily" })).rejects.toThrow(/Tavily.*rejected/i);
    } finally {
      globalThis.fetch = originalFetch;
      if (origKey === undefined) {
        // biome-ignore lint/performance/noDelete: same reason
        delete process.env.TAVILY_API_KEY;
      } else {
        process.env.TAVILY_API_KEY = origKey;
      }
    }
  });

  it("maps 429 to a quota/rate-limit error", async () => {
    const origKey = process.env.TAVILY_API_KEY;
    process.env.TAVILY_API_KEY = "tvly-test-key";
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(
      async () => new Response("too many", { status: 429 }),
    ) as unknown as typeof fetch;
    try {
      await expect(webSearch("q", { engine: "tavily" })).rejects.toThrow(/quota|rate-limit/i);
    } finally {
      globalThis.fetch = originalFetch;
      if (origKey === undefined) {
        // biome-ignore lint/performance/noDelete: same reason
        delete process.env.TAVILY_API_KEY;
      } else {
        process.env.TAVILY_API_KEY = origKey;
      }
    }
  });

  it("returns empty array when results is empty", async () => {
    const origKey = process.env.TAVILY_API_KEY;
    process.env.TAVILY_API_KEY = "tvly-test-key";
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(
      async () =>
        new Response(JSON.stringify({ query: "x", results: [] }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
    ) as unknown as typeof fetch;
    try {
      const out = await webSearch("x", { engine: "tavily" });
      expect(out).toEqual([]);
    } finally {
      globalThis.fetch = originalFetch;
      if (origKey === undefined) {
        // biome-ignore lint/performance/noDelete: same reason
        delete process.env.TAVILY_API_KEY;
      } else {
        process.env.TAVILY_API_KEY = origKey;
      }
    }
  });
});

describe("searchPerplexity", () => {
  const sampleResponse = {
    choices: [{ message: { content: "Synthesized answer about the query." } }],
    citations: [
      { url: "https://source.example.com/a", title: "Source A" },
      "https://source.example.com/b",
    ],
  };

  it("requires an API key — throws a setup-pointing error when none is set", async () => {
    const origKey = process.env.PERPLEXITY_API_KEY;
    // biome-ignore lint/performance/noDelete: env var must be absent, not "undefined"
    delete process.env.PERPLEXITY_API_KEY;
    try {
      await expect(webSearch("q", { engine: "perplexity" })).rejects.toThrow(
        /Perplexity.*API key/i,
      );
      await expect(webSearch("q", { engine: "perplexity" })).rejects.toThrow("perplexity.ai");
    } finally {
      if (origKey !== undefined) process.env.PERPLEXITY_API_KEY = origKey;
    }
  });

  it("POSTs to Perplexity with bearer auth and sonar model", async () => {
    const origKey = process.env.PERPLEXITY_API_KEY;
    process.env.PERPLEXITY_API_KEY = "pplx-test-key";
    const captured: { url: string; method: string; auth: string; body: string } = {
      url: "",
      method: "",
      auth: "",
      body: "",
    };
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(async (url: string | URL, init?: RequestInit) => {
      captured.url = String(url);
      captured.method = init?.method ?? "GET";
      captured.auth = String((init?.headers as Record<string, string>)?.Authorization ?? "");
      captured.body = String(init?.body ?? "");
      return new Response(JSON.stringify(sampleResponse), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }) as unknown as typeof fetch;
    try {
      const out = await webSearch("test query", { engine: "perplexity", topK: 5 });
      expect(captured.url).toBe("https://api.perplexity.ai/chat/completions");
      expect(captured.method).toBe("POST");
      expect(captured.auth).toBe("Bearer pplx-test-key");
      const body = JSON.parse(captured.body);
      expect(body.model).toBe("sonar");
      expect(body.messages[0].content).toBe("test query");
      // First result is the AI answer (url:"" sentinel + answer field)
      expect(out[0]).toEqual({
        title: "Synthesized answer about the query.",
        url: "",
        snippet: "",
        answer: "Synthesized answer about the query.",
      });
      // Citations follow — both object and string forms
      expect(out[1]?.url).toBe("https://source.example.com/a");
      expect(out[1]?.title).toBe("Source A");
      expect(out[2]?.url).toBe("https://source.example.com/b");
    } finally {
      globalThis.fetch = originalFetch;
      if (origKey === undefined) {
        // biome-ignore lint/performance/noDelete: same reason
        delete process.env.PERPLEXITY_API_KEY;
      } else {
        process.env.PERPLEXITY_API_KEY = origKey;
      }
    }
  });

  it("maps 401/403 to a key-rejected error", async () => {
    const origKey = process.env.PERPLEXITY_API_KEY;
    process.env.PERPLEXITY_API_KEY = "pplx-bad";
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(
      async () => new Response("forbidden", { status: 403 }),
    ) as unknown as typeof fetch;
    try {
      await expect(webSearch("q", { engine: "perplexity" })).rejects.toThrow(
        /Perplexity.*rejected/i,
      );
    } finally {
      globalThis.fetch = originalFetch;
      if (origKey === undefined) {
        // biome-ignore lint/performance/noDelete: same reason
        delete process.env.PERPLEXITY_API_KEY;
      } else {
        process.env.PERPLEXITY_API_KEY = origKey;
      }
    }
  });

  it("maps 429 to a rate-limit error", async () => {
    const origKey = process.env.PERPLEXITY_API_KEY;
    process.env.PERPLEXITY_API_KEY = "pplx-test-key";
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(
      async () => new Response("too many", { status: 429 }),
    ) as unknown as typeof fetch;
    try {
      await expect(webSearch("q", { engine: "perplexity" })).rejects.toThrow(/rate-limit/i);
    } finally {
      globalThis.fetch = originalFetch;
      if (origKey === undefined) {
        // biome-ignore lint/performance/noDelete: same reason
        delete process.env.PERPLEXITY_API_KEY;
      } else {
        process.env.PERPLEXITY_API_KEY = origKey;
      }
    }
  });

  it("maps unparseable JSON to a parse error", async () => {
    const origKey = process.env.PERPLEXITY_API_KEY;
    process.env.PERPLEXITY_API_KEY = "pplx-test-key";
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(
      async () => new Response("<html>not json</html>", { status: 200 }),
    ) as unknown as typeof fetch;
    try {
      await expect(webSearch("q", { engine: "perplexity" })).rejects.toThrow(/unparseable/i);
    } finally {
      globalThis.fetch = originalFetch;
      if (origKey === undefined) {
        // biome-ignore lint/performance/noDelete: same reason
        delete process.env.PERPLEXITY_API_KEY;
      } else {
        process.env.PERPLEXITY_API_KEY = origKey;
      }
    }
  });
});

describe("searchExa", () => {
  const sampleResponse = {
    answer: "Exa synthesized answer.",
    citations: [
      {
        title: "Citation A",
        url: "https://exa.example.com/a",
        text: "Snippet A.",
      },
      {
        title: "Citation B",
        url: "https://exa.example.com/b",
        text: "Snippet B.",
      },
    ],
  };

  it("requires an API key — throws a setup-pointing error when none is set", async () => {
    const origKey = process.env.EXA_API_KEY;
    // biome-ignore lint/performance/noDelete: env var must be absent, not "undefined"
    delete process.env.EXA_API_KEY;
    try {
      await expect(webSearch("q", { engine: "exa" })).rejects.toThrow(/Exa.*API key/i);
      await expect(webSearch("q", { engine: "exa" })).rejects.toThrow("exa.ai");
    } finally {
      if (origKey !== undefined) process.env.EXA_API_KEY = origKey;
    }
  });

  it("POSTs to Exa /answer with x-api-key header", async () => {
    const origKey = process.env.EXA_API_KEY;
    process.env.EXA_API_KEY = "exa-test-key";
    const captured: { url: string; method: string; auth: string; body: string } = {
      url: "",
      method: "",
      auth: "",
      body: "",
    };
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(async (url: string | URL, init?: RequestInit) => {
      captured.url = String(url);
      captured.method = init?.method ?? "GET";
      captured.auth = String((init?.headers as Record<string, string>)?.["x-api-key"] ?? "");
      captured.body = String(init?.body ?? "");
      return new Response(JSON.stringify(sampleResponse), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }) as unknown as typeof fetch;
    try {
      const out = await webSearch("test query", { engine: "exa", topK: 5 });
      expect(captured.url).toBe("https://api.exa.ai/answer");
      expect(captured.method).toBe("POST");
      expect(captured.auth).toBe("exa-test-key");
      const body = JSON.parse(captured.body);
      expect(body.query).toBe("test query");
      expect(out[0]).toEqual({
        title: "Exa synthesized answer.",
        url: "",
        snippet: "",
        answer: "Exa synthesized answer.",
      });
      expect(out[1]).toEqual({
        title: "Citation A",
        url: "https://exa.example.com/a",
        snippet: "Snippet A.",
      });
    } finally {
      globalThis.fetch = originalFetch;
      if (origKey === undefined) {
        // biome-ignore lint/performance/noDelete: same reason
        delete process.env.EXA_API_KEY;
      } else {
        process.env.EXA_API_KEY = origKey;
      }
    }
  });

  it("maps 401/403 to a key-rejected error", async () => {
    const origKey = process.env.EXA_API_KEY;
    process.env.EXA_API_KEY = "exa-bad";
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(
      async () => new Response("forbidden", { status: 403 }),
    ) as unknown as typeof fetch;
    try {
      await expect(webSearch("q", { engine: "exa" })).rejects.toThrow(/Exa.*rejected/i);
    } finally {
      globalThis.fetch = originalFetch;
      if (origKey === undefined) {
        // biome-ignore lint/performance/noDelete: same reason
        delete process.env.EXA_API_KEY;
      } else {
        process.env.EXA_API_KEY = origKey;
      }
    }
  });

  it("maps 429 to a rate-limit/quota error", async () => {
    const origKey = process.env.EXA_API_KEY;
    process.env.EXA_API_KEY = "exa-test-key";
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(
      async () => new Response("too many", { status: 429 }),
    ) as unknown as typeof fetch;
    try {
      await expect(webSearch("q", { engine: "exa" })).rejects.toThrow(/rate-limit|quota/i);
    } finally {
      globalThis.fetch = originalFetch;
      if (origKey === undefined) {
        // biome-ignore lint/performance/noDelete: same reason
        delete process.env.EXA_API_KEY;
      } else {
        process.env.EXA_API_KEY = origKey;
      }
    }
  });

  it("maps unparseable JSON to a parse error", async () => {
    const origKey = process.env.EXA_API_KEY;
    process.env.EXA_API_KEY = "exa-test-key";
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(
      async () => new Response("<html>not json</html>", { status: 200 }),
    ) as unknown as typeof fetch;
    try {
      await expect(webSearch("q", { engine: "exa" })).rejects.toThrow(/unparseable/i);
    } finally {
      globalThis.fetch = originalFetch;
      if (origKey === undefined) {
        // biome-ignore lint/performance/noDelete: same reason
        delete process.env.EXA_API_KEY;
      } else {
        process.env.EXA_API_KEY = origKey;
      }
    }
  });
});

describe("formatSearchResults", () => {
  it("renders a query header + numbered list", () => {
    const out = formatSearchResults("hello", [
      { title: "One", url: "https://one", snippet: "first" },
      { title: "Two", url: "https://two", snippet: "second" },
    ]);
    expect(out).toMatch(/query: hello/);
    expect(out).toMatch(/results \(2\)/);
    expect(out).toMatch(/1\. One\n\s+https:\/\/one\n\s+first/);
    expect(out).toMatch(/2\. Two/);
  });

  it("renders answer + sources dual-section when an AI answer is present", () => {
    const out = formatSearchResults("hello", [
      { title: "An AI answer.", url: "", snippet: "", answer: "An AI answer." },
      { title: "Citation A", url: "https://a", snippet: "snippet a" },
      { title: "Citation B", url: "https://b", snippet: "" },
    ]);
    expect(out).toMatch(/answer:\n\s+An AI answer\./);
    expect(out).toMatch(/sources \(2\)/);
    expect(out).toMatch(/1\. Citation A\n\s+https:\/\/a\n\s+snippet a/);
    expect(out).toMatch(/2\. Citation B/);
    expect(out).not.toMatch(/^results \(/m);
  });
});

describe("registerWebTools", () => {
  const isolatedConfigPath = "/tmp/reasonix-web-tools-test-config-does-not-exist.json";

  it("registers web_search and web_fetch", () => {
    const registry = new ToolRegistry();
    registerWebTools(registry, { configPath: isolatedConfigPath });
    expect(registry.size).toBe(2);
  });

  it("web_fetch refuses non-http(s) urls", async () => {
    const registry = new ToolRegistry();
    registerWebTools(registry, { configPath: isolatedConfigPath });
    const out = await registry.dispatch("web_fetch", JSON.stringify({ url: "file:///etc/passwd" }));
    expect(out).toMatch(/must start with http/);
  });

  it("web_search dispatch returns formatted results", async () => {
    const html = `
      <li class="b_algo"><h2><a href="https://example.com/a">A</a></h2>
        <div class="b_caption"><p>snippet A</p></div></li>
      <li class="b_algo"><h2><a href="https://example.com/b">B</a></h2>
        <div class="b_caption"><p>snippet B</p></div></li>`;
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(
      async () => new Response(html, { status: 200, headers: { "Content-Type": "text/html" } }),
    ) as unknown as typeof fetch;
    try {
      const registry = new ToolRegistry();
      registerWebTools(registry, { configPath: isolatedConfigPath });
      const out = await registry.dispatch(
        "web_search",
        JSON.stringify({ query: "flutter 3.19", topK: 2 }),
      );
      expect(out).toContain("query: flutter 3.19");
      expect(out).toContain("https://example.com/a");
      expect(out).toContain("snippet A");
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  it("web_fetch dispatch returns title + body text", async () => {
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(
      async () =>
        new Response(
          "<html><head><title>Demo</title></head><body><p>Hello world.</p></body></html>",
          { status: 200, headers: { "Content-Type": "text/html; charset=utf-8" } },
        ),
    ) as unknown as typeof fetch;
    try {
      const registry = new ToolRegistry();
      registerWebTools(registry, { configPath: isolatedConfigPath });
      const out = await registry.dispatch(
        "web_fetch",
        JSON.stringify({ url: "https://example.com/" }),
      );
      expect(out).toContain("Demo");
      expect(out).toContain("https://example.com/");
      expect(out).toContain("Hello world.");
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  it("web_search can dispatch through Ollama cloud", async () => {
    const configPath = "/tmp/reasonix-web-tools-ollama-search-test.json";
    writeConfig({ webSearchEngine: "ollama", ollamaApiKey: "ollama-test-key" }, configPath);
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(async (_url, init) => {
      expect(_url).toBe("https://ollama.com/api/web_search");
      expect((init as RequestInit).headers).toMatchObject({
        Authorization: "Bearer ollama-test-key",
      });
      expect(JSON.parse((init as RequestInit).body as string)).toMatchObject({
        query: "reasonix",
        max_results: 2,
      });
      return new Response(
        JSON.stringify({
          results: [{ title: "Reasonix", url: "https://example.com/r", content: "Result body" }],
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    }) as unknown as typeof fetch;
    try {
      const registry = new ToolRegistry();
      registerWebTools(registry, { configPath });
      const out = await registry.dispatch(
        "web_search",
        JSON.stringify({ query: "reasonix", topK: 2 }),
      );
      expect(out).toContain("https://example.com/r");
      expect(out).toContain("Result body");
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  it("web_fetch can dispatch through Ollama cloud when selected", async () => {
    const configPath = "/tmp/reasonix-web-tools-ollama-fetch-test.json";
    writeConfig({ webSearchEngine: "ollama", ollamaApiKey: "ollama-test-key" }, configPath);
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(async (_url, init) => {
      expect(_url).toBe("https://ollama.com/api/web_fetch");
      expect((init as RequestInit).headers).toMatchObject({
        Authorization: "Bearer ollama-test-key",
      });
      expect(JSON.parse((init as RequestInit).body as string)).toMatchObject({
        url: "https://example.com/page",
      });
      return new Response(
        JSON.stringify({
          title: "Fetched",
          content: "Fetched content",
          links: ["https://example.com/next"],
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    }) as unknown as typeof fetch;
    try {
      const registry = new ToolRegistry();
      registerWebTools(registry, { configPath });
      const out = await registry.dispatch(
        "web_fetch",
        JSON.stringify({ url: "https://example.com/page" }),
      );
      expect(out).toContain("Fetched");
      expect(out).toContain("Fetched content");
      expect(out).toContain("https://example.com/next");
    } finally {
      globalThis.fetch = originalFetch;
    }
  });
});

describe("webFetch", () => {
  const mockedLookup = vi.mocked(lookup);

  beforeEach(() => {
    mockedLookup.mockResolvedValue([{ address: "93.184.216.34", family: 4 }]);
  });

  it("extracts title + body text from an html response", async () => {
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(
      async () =>
        new Response(
          "<html><head><title>Demo</title></head><body><p>Hello world.</p></body></html>",
          { status: 200, headers: { "Content-Type": "text/html; charset=utf-8" } },
        ),
    ) as unknown as typeof fetch;
    try {
      const page = await webFetch("https://example.com/demo");
      expect(page.title).toBe("Demo");
      expect(page.text).toContain("Hello world.");
      expect(page.text).not.toContain("<title>");
      expect(page.truncated).toBe(false);
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  it("truncates long pages and flags the result", async () => {
    const originalFetch = globalThis.fetch;
    const big = `<html><body><p>${"a".repeat(50_000)}</p></body></html>`;
    globalThis.fetch = vi.fn(
      async () => new Response(big, { status: 200, headers: { "Content-Type": "text/html" } }),
    ) as unknown as typeof fetch;
    try {
      const page = await webFetch("https://example.com/big", { maxChars: 1000 });
      expect(page.truncated).toBe(true);
      expect(page.text).toMatch(/\[… truncated \d+ chars …\]$/);
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  it("surfaces non-2xx as a thrown error", async () => {
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(
      async () => new Response("nope", { status: 404 }),
    ) as unknown as typeof fetch;
    try {
      await expect(webFetch("https://example.com/missing")).rejects.toThrow(/web_fetch 404/);
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  it("refuses literal internal addresses before fetching", async () => {
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn() as unknown as typeof fetch;
    try {
      await expect(webFetch("http://127.0.0.1:9200/")).rejects.toThrow(
        /refuses internal or reserved host: 127\.0\.0\.1/,
      );
      expect(globalThis.fetch).not.toHaveBeenCalled();
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  it("refuses DNS names that resolve to internal addresses", async () => {
    mockedLookup.mockResolvedValueOnce([{ address: "10.0.0.5", family: 4 }]);
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn() as unknown as typeof fetch;
    try {
      await expect(webFetch("https://metadata.example/")).rejects.toThrow(
        /refuses internal or reserved host: metadata\.example/,
      );
      expect(globalThis.fetch).not.toHaveBeenCalled();
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  it("refuses redirects to internal hosts", async () => {
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(
      async () =>
        new Response("", { status: 302, headers: { Location: "http://169.254.169.254/" } }),
    ) as unknown as typeof fetch;
    try {
      await expect(webFetch("https://example.com/redirect")).rejects.toThrow(
        /refuses internal or reserved host: 169\.254\.169\.254/,
      );
      expect(globalThis.fetch).toHaveBeenCalledTimes(1);
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  it("refuses upfront when Content-Length declares a body past the byte cap", async () => {
    const originalFetch = globalThis.fetch;
    // 50MB declared — well past the 10MB cap. Body text doesn't even
    // need to match; the pre-flight check fires before we read it.
    globalThis.fetch = vi.fn(
      async () =>
        new Response("ignored", {
          status: 200,
          headers: { "Content-Type": "text/html", "Content-Length": String(50 * 1024 * 1024) },
        }),
    ) as unknown as typeof fetch;
    try {
      await expect(webFetch("https://example.com/big.iso")).rejects.toThrow(
        /content-length .* exceeds .* cap/,
      );
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  it("annotates 429 with a wait-and-retry hint", async () => {
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(
      async () => new Response("nope", { status: 429 }),
    ) as unknown as typeof fetch;
    try {
      await expect(webFetch("https://example.com/rate")).rejects.toThrow(
        /web_fetch 429.*try:.*wait/,
      );
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  it("annotates 403 with a hint that the host is blocking", async () => {
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(
      async () => new Response("nope", { status: 403 }),
    ) as unknown as typeof fetch;
    try {
      await expect(webFetch("https://example.com/forbidden")).rejects.toThrow(
        /web_fetch 403.*try:.*blocking|web_fetch 403.*try:/,
      );
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  it("annotates 5xx with a transient-retry hint", async () => {
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(
      async () => new Response("upstream broken", { status: 502 }),
    ) as unknown as typeof fetch;
    try {
      await expect(webFetch("https://example.com/gateway")).rejects.toThrow(
        /web_fetch 502.*try:.*retry in 30s/,
      );
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  it("throws a timeout-with-hint message when the internal timer fires", async () => {
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(async (_url: string | URL, init?: RequestInit) => {
      // Hang until the caller (or our timer) aborts, then surface that
      // as an AbortError exactly like real fetch does.
      return await new Promise<Response>((_, reject) => {
        const sig = init?.signal;
        if (sig?.aborted) {
          reject(new DOMException("aborted", "AbortError"));
          return;
        }
        sig?.addEventListener("abort", () => {
          reject(new DOMException("aborted", "AbortError"));
        });
      });
    }) as unknown as typeof fetch;
    try {
      await expect(webFetch("https://example.com/slow", { timeoutMs: 10 })).rejects.toThrow(
        /timed out after 10ms.*try:/,
      );
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  it("does not conflate caller-cancelled aborts with the timeout hint", async () => {
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(async (_url: string | URL, init?: RequestInit) => {
      return await new Promise<Response>((_, reject) => {
        init?.signal?.addEventListener("abort", () => {
          reject(new DOMException("aborted", "AbortError"));
        });
      });
    }) as unknown as typeof fetch;
    try {
      const ctl = new AbortController();
      const p = webFetch("https://example.com/cancelled", {
        timeoutMs: 60_000,
        signal: ctl.signal,
      });
      ctl.abort();
      await expect(p).rejects.toThrow();
      // The thrown error should NOT be the timeout hint.
      await expect(p).rejects.not.toThrow(/timed out after/);
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  it("aborts mid-stream when an undeclared body crosses the byte cap", async () => {
    // No Content-Length header → pre-flight passes; the streaming
    // reader has to enforce the cap. Stream pushes 1MB chunks past
    // the 10MB cap.
    const originalFetch = globalThis.fetch;
    const chunk = new Uint8Array(1024 * 1024).fill(65); // 1MB of 'A'
    const stream = new ReadableStream<Uint8Array>({
      pull(controller) {
        // 12 chunks → 12MB, past the 10MB cap.
        for (let i = 0; i < 12; i++) controller.enqueue(chunk);
        controller.close();
      },
    });
    globalThis.fetch = vi.fn(
      async () =>
        new Response(stream, {
          status: 200,
          headers: { "Content-Type": "text/html" },
        }),
    ) as unknown as typeof fetch;
    try {
      await expect(webFetch("https://example.com/chunked")).rejects.toThrow(
        /response body exceeded .* cap/,
      );
    } finally {
      globalThis.fetch = originalFetch;
    }
  });
});
