/**
 * Null-safe formatting. Every function here treats `null`/`undefined` as
 * "no data" and returns the DASH sentinel — it must never render as 0 or
 * 0%. See docs/FRONTEND_ARCHITECTURE.md §6.
 */

export const DASH = '—';

export function formatMs(ms: number | null | undefined): string {
  if (ms == null) return DASH;
  if (ms < 1000) return `${Math.round(ms)}ms`;
  return `${(ms / 1000).toFixed(2)}s`;
}

export function formatPct(ratio: number | null | undefined, digits = 1): string {
  if (ratio == null) return DASH;
  return `${(ratio * 100).toFixed(digits)}%`;
}

/** Compact integer formatting: 1234 -> "1.2k", 1200000 -> "1.2M". Exact
 * values under 1000 are shown as-is since precision matters more there. */
export function formatCount(n: number | null | undefined): string {
  if (n == null) return DASH;
  if (Math.abs(n) < 1000) return n.toLocaleString();
  const units: [number, string][] = [
    [1_000_000_000, 'B'],
    [1_000_000, 'M'],
    [1_000, 'k'],
  ];
  for (const [threshold, suffix] of units) {
    if (Math.abs(n) >= threshold) {
      return `${(n / threshold).toFixed(1).replace(/\.0$/, '')}${suffix}`;
    }
  }
  return n.toLocaleString();
}

export function formatMultiplier(x: number | null | undefined): string {
  if (x == null) return DASH;
  return `${x.toFixed(1)}×`;
}

export function formatTimestamp(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return DASH;
  return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

export function formatDateTime(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return DASH;
  return d.toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
}
