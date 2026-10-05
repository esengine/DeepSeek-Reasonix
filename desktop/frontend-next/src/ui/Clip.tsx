/** Clip is one line that may not fit. The whole string rides on `title`, so a
 *  narrow window shortens what is read rather than taking it away: an ellipsis
 *  with nothing behind it is the one form of truncation a reader cannot undo. */
export function Clip({ className, children }: { className?: string; children: string }) {
  return (
    <span className={className} title={children}>
      {children}
    </span>
  );
}
