export function niceMax(raw: number): number {
  if (raw <= 0) return 2;
  const pow = Math.pow(10, Math.floor(Math.log10(raw)));
  return [1, 1.2, 1.5, 2, 2.5, 3, 4, 5, 6, 8, 10].map((m) => m * pow).find((v) => v >= raw) ?? raw;
}
