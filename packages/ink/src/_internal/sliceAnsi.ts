// Thin indirection over the upstream `slice-ansi` package. Keeping the
// dependency behind a local module means we can swap implementations
// (e.g. for a custom width-aware slicer) without touching every call site.
import sliceAnsi from 'slice-ansi';

export default sliceAnsi;
