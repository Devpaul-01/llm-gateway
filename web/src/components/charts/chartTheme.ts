/** Shared chart theming read from the CSS custom properties in tokens.css,
 * so Recharts (which needs real color strings, not var()) stays in sync
 * with the rest of the UI without duplicating hex values here. */
export function cssVar(name: string): string {
  if (typeof window === 'undefined') return '#000';
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

export const chartColors = {
  get accent() {
    return cssVar('--accent');
  },
  get warn() {
    return cssVar('--warn');
  },
  get error() {
    return cssVar('--error');
  },
  get grid() {
    return cssVar('--border');
  },
  get axisText() {
    return cssVar('--text-faint');
  },
  get tooltipBg() {
    return cssVar('--surface-raised');
  },
  get tooltipBorder() {
    return cssVar('--border-strong');
  },
  // Validated categorical order — see tokens.css for the provenance note.
  categorical: [
    'var(--chart-1)',
    'var(--chart-2)',
    'var(--chart-3)',
    'var(--chart-4)',
    'var(--chart-5)',
    'var(--chart-6)',
    'var(--chart-7)',
    'var(--chart-8)',
  ],
};

export const CATEGORICAL_SERIES_CAP = 8;
