import type { ReactNode } from 'react';
import { DASH } from '../../lib/format';
import './StatTile.css';

export type StatTone = 'neutral' | 'good' | 'warn' | 'bad';

interface StatTileProps {
  label: string;
  value: string;
  tone?: StatTone;
  hint?: string;
  size?: 'lg' | 'md';
  footnote?: ReactNode;
}

/** The hero numeric display. `value` must already be formatted by the
 * caller via src/lib/format.ts, so DASH flows through consistently —
 * this component never substitutes its own 0 for a missing value. */
export function StatTile({ label, value, tone = 'neutral', hint, size = 'md', footnote }: StatTileProps) {
  const isEmpty = value === DASH;
  return (
    <div className="stat-tile" title={hint}>
      <span className="stat-tile__label">{label}</span>
      <span
        className={`stat-tile__value stat-tile__value--${size} stat-tile__value--${isEmpty ? 'neutral' : tone}`}
      >
        {value}
      </span>
      {footnote && <span className="stat-tile__footnote">{footnote}</span>}
    </div>
  );
}
