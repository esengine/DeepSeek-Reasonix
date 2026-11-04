export function Hit({ text, needle }: { text: string; needle: string }) {
  if (!needle) return <>{text}</>;
  const at = text.toLowerCase().indexOf(needle.toLowerCase());
  if (at < 0) return <>{text}</>;
  const end = at + needle.length;
  return <>{text.slice(0, at)}<mark className="hit">{text.slice(at, end)}</mark>{text.slice(end)}</>;
}
