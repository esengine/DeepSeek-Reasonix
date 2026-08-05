// Loader hook for tsx-based tests: static asset imports (SVG logos, etc.)
// resolve to an empty string default export so component tests can import
// components whose transitive store/component chain pulls in an asset.
export async function load(url, context, nextLoad) {
  if (url.endsWith(".svg") || url.endsWith(".png") || url.endsWith(".webp")) {
    return { format: "module", source: "export default \"\";", shortCircuit: true };
  }
  return nextLoad(url, context);
}
