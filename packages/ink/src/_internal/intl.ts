// `Intl.Segmenter` construction is surprisingly expensive — measurable on
// the renderer hot path when called per-character. One module-level instance
// is enough: the segmenter is stateless across `.segment()` calls and we
// only ever need grapheme granularity for terminal width calculations.

let cachedSegmenter: Intl.Segmenter | null = null;

export function getGraphemeSegmenter(): Intl.Segmenter {
  if (!cachedSegmenter) {
    cachedSegmenter = new Intl.Segmenter('en', { granularity: 'grapheme' });
  }
  return cachedSegmenter;
}
