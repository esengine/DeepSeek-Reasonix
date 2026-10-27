// vendormarks.tsx — vendor artwork keyed by the slug a preset declares.
const FILES = import.meta.glob("../assets/vendor-marks/*.svg", { query: "?raw", import: "default", eager: true }) as Record<string, string>;

const MARKS: Record<string, { file: string; name: string }> = {
  deepseek: { file: "deepseek-color", name: "DeepSeek" },
  zhipu: { file: "zhipu-color", name: "Zhipu" },
  kimi: { file: "kimi-color", name: "Kimi" },
  minimax: { file: "minimax-color", name: "MiniMax" },
  qwen: { file: "qwen-color", name: "Qwen" },
  mimo: { file: "xiaomimimo", name: "MiMo" },
  longcat: { file: "longcat-color", name: "LongCat" },
  stepfun: { file: "stepfun-color", name: "StepFun" },
  openrouter: { file: "openrouter", name: "OpenRouter" },
  openai: { file: "openai", name: "OpenAI" },
  google: { file: "gemini-color", name: "Google" },
  nvidia: { file: "nvidia-color", name: "NVIDIA" },
  ollama: { file: "ollama", name: "Ollama" },
  volcengine: { file: "volcengine-color", name: "Volcengine" },
  opencode: { file: "opencode", name: "OpenCode" },
  anthropic: { file: "anthropic", name: "Anthropic" },
  meta: { file: "meta-color", name: "Meta" },
  mistral: { file: "mistral-color", name: "Mistral" },
  perplexity: { file: "perplexity-color", name: "Perplexity" },
  azure: { file: "azure-color", name: "Azure" },
  hunyuan: { file: "hunyuan-color", name: "Hunyuan" },
  doubao: { file: "doubao-color", name: "Doubao" },
};

const SOURCE = new Map<string, string>();
for (const [slug, { file }] of Object.entries(MARKS)) {
  const raw = FILES[`../assets/vendor-marks/${file}.svg`];
  if (raw) SOURCE.set(slug, raw.replace(/<title>.*?<\/title>/, "").replace(/ style="[^"]*"/, ""));
}

export function hasVendorMark(slug: string | undefined): slug is string {
  return !!slug && SOURCE.has(slug);
}

export function vendorName(slug: string): string {
  return MARKS[slug]?.name ?? slug;
}

/** Slugs in declaration order, for galleries that list every known vendor. */
export function vendorSlugs(): string[] {
  return [...SOURCE.keys()];
}

export function VendorMark({ slug, size = 18 }: { slug: string; size?: number }) {
  const svg = SOURCE.get(slug);
  if (!svg) return null;
  return <i className="vendor-mark" data-mark={slug} style={{ width: size, height: size }} aria-hidden="true" dangerouslySetInnerHTML={{ __html: svg }} />;
}
