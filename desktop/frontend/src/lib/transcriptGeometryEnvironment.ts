import type { TranscriptGeometryEnvironment } from "./transcriptRowGeometry";

const FALLBACK_CONTENT_WIDTH = 960;

function finitePixel(value: string | undefined): number | undefined {
  const parsed = Number.parseFloat(value ?? "");
  return Number.isFinite(parsed) && parsed > 0 ? parsed : undefined;
}

function compactToken(value: string | undefined): string {
  return (value ?? "").trim().replace(/\s+/g, " ").slice(0, 240);
}

/** Reads the same semantic font regions and readable column that row CSS uses. */
export function readTranscriptGeometryEnvironment(element: HTMLElement): TranscriptGeometryEnvironment {
  const rawWidth = element.clientWidth || element.getBoundingClientRect().width;
  const elementWidth = Number.isFinite(rawWidth) && rawWidth > 0 ? rawWidth : FALLBACK_CONTENT_WIDTH;
  const style = getComputedStyle(element);
  const rootStyle = getComputedStyle(document.documentElement);
  const maxWidth = finitePixel(rootStyle.getPropertyValue("--maxw")) ?? FALLBACK_CONTENT_WIDTH;
  const inlinePadding = finitePixel(style.getPropertyValue("--transcript-inline-pad"))
    ?? finitePixel(rootStyle.getPropertyValue("--transcript-inline-pad"))
    ?? 0;
  const availableWidth = Math.max(1, elementWidth - inlinePadding * 2);
  const contentWidth = Math.round(Math.min(maxWidth, availableWidth));
  const signature = [
    compactToken(style.fontFamily),
    compactToken(style.fontSize),
    compactToken(style.lineHeight),
    compactToken(rootStyle.getPropertyValue("--typography-conversation-font")),
    compactToken(rootStyle.getPropertyValue("--typography-conversation-size")),
    compactToken(rootStyle.getPropertyValue("--typography-code-font")),
    compactToken(rootStyle.getPropertyValue("--typography-code-size")),
    compactToken(rootStyle.getPropertyValue("--typography-metadata-font")),
    compactToken(rootStyle.getPropertyValue("--typography-metadata-size")),
  ].join("|");
  return { contentWidth, typographySignature: signature || "default" };
}
