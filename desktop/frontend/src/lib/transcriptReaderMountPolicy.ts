// Keep moderately sized transcripts mounted as one stable window. Replacing
// non-overlapping Virtuoso ranges while Markdown heights settle can otherwise
// move the native scroll extent underneath an active reading gesture.
// Larger sessions retain the bounded reader corridor to cap DOM growth.
export const TRANSCRIPT_STATIC_WINDOW_ROW_LIMIT = 1_000;
