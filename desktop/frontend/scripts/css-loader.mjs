// Loader hook for tsx-based tests: `.css` imports resolve to an empty module
// so component modules (e.g. HeartbeatPanel) can statically import their CSS
// without failing under node. Vite handles the real CSS in browser builds.
export async function load(url, context, nextLoad) {
  if (url.endsWith(".css")) {
    return { format: "module", source: "export default \"\";", shortCircuit: true };
  }
  return nextLoad(url, context);
}
