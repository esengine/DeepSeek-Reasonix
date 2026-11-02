const WORD_CSS = "body{margin:0;background:#e8e8e8}"
  + ".docx-wrapper{background:#e8e8e8;padding:16px 0}"
  + ".docx-wrapper>section.docx{margin:0 auto 16px;box-shadow:0 1px 4px rgba(0,0,0,.2)}";

// Word draws bullets with private-use code points from the Symbol and Wingdings
// fonts, which the preview cannot load; these are the Unicode glyphs they stand for.
const SYMBOL_BULLETS: Record<string, string> = {
  "": "•", "": "▪", "": "□", "": "●", "": "■",
  "": "➢", "": "❖", "": "✓", "": "➔", "": "◇",
};

export interface WordView {
  html: string;
  /** The first page's width in CSS pixels, 0 when the file declares none. */
  pageWidth: number;
}

function pixels(width: string): number {
  const m = /^([\d.]+)(pt|px|in|cm|mm)$/.exec(width.trim());
  if (!m) return 0;
  const per: Record<string, number> = { pt: 4 / 3, px: 1, in: 96, cm: 96 / 2.54, mm: 96 / 25.4 };
  return Number(m[1]) * per[m[2]];
}

/** A Word file as static HTML: images inlined as data URLs, nothing fetched later. */
export async function readWordDocument(data: ArrayBuffer): Promise<WordView> {
  const { renderAsync } = await import("docx-preview");
  const body = document.createElement("div");
  const styles = document.createElement("div");
  await renderAsync(data, body, styles, {
    inWrapper: true,
    breakPages: true,
    ignoreLastRenderedPageBreak: true,
    useBase64URL: true,
    renderHeaders: true,
    renderFooters: true,
    renderFootnotes: true,
  });
  const page = body.querySelector<HTMLElement>("section.docx");
  const html = `<style>${WORD_CSS}</style>${styles.innerHTML}${body.innerHTML}`
    .replace(/[-]/g, (c) => SYMBOL_BULLETS[c] ?? c);
  return { html, pageWidth: page ? pixels(page.style.width) : 0 };
}

/** The zoom that fits a page of pageWidth into a frame of frameWidth, in 5% steps. */
export function fitZoom(frameWidth: number, pageWidth: number): number {
  if (pageWidth <= 0 || frameWidth <= 0) return 1;
  return Math.max(0.25, Math.min(1, Math.floor(((frameWidth - 32) / pageWidth) * 20) / 20));
}
