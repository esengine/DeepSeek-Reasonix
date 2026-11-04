// The picker's own mark: a frame with a pointer, the way "choose an element on
// this page" is drawn in a browser's inspector.
export function PickIcon({ size = 14 }: { size?: number }) {
  return (
    <svg
      viewBox="0 0 16 16"
      width={size}
      height={size}
      fill="none"
      stroke="currentColor"
      strokeWidth="1.3"
      aria-hidden="true"
    >
      <rect x="1.5" y="1.5" width="9" height="9" rx="1" strokeDasharray="2 1.5" />
      <path d="M9.2 8.2l5.3 2.1-2.2 1.2-1.2 2.2z" fill="currentColor" stroke="none" />
    </svg>
  );
}
