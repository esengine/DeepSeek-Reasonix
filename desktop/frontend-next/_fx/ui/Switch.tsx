// A generic control, at the path the census declares primitives at. Its own
// handler is not an entry point: what a person clicks is the call site that
// hands it one, and counting the primitive's internal button as a root would
// give it the join of every usage in the product.
export function Switch({ onClick, ...rest }: { onClick: () => void } & Record<string, unknown>) {
  return (
    <button {...rest} onClick={onClick}>
      s
    </button>
  );
}
