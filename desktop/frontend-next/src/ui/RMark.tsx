export function RMark({ className = "rmark" }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 16 16" aria-hidden="true">
      <path pathLength={100} d="M4.7 2.9V13.1" />
      <path pathLength={100} d="M4.7 2.9h4.2a2.9 2.9 0 0 1 0 5.8H4.7" />
      <path pathLength={100} d="M9 8.7l3.3 4.4" />
    </svg>
  );
}
