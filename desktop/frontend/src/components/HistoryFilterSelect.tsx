export function HistoryFilterSelect({
  label,
  options,
  value,
  onChange,
}: {
  label: string;
  options: { id: string; label: string; count: number }[];
  value: string;
  onChange: (next: string) => void;
}) {
  const visibleOptions = options.filter((option) => option.id === "all" || option.id === value || option.count > 0);
  return (
    <div className="history-filter" role="group" aria-label={label}>
      {visibleOptions.map((option) => (
        <button
          key={option.id}
          type="button"
          className={`history-filter__pill${value === option.id ? " history-filter__pill--on" : ""}`}
          aria-pressed={value === option.id}
          disabled={option.id !== "all" && option.count === 0}
          onClick={() => onChange(option.id)}
        >
          {option.label}
          <span className="history-filter__count">{option.count}</span>
        </button>
      ))}
    </div>
  );
}
