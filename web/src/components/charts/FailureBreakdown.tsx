import type { FailureCategory } from '../../api/types';
import { formatCount } from '../../lib/format';
import './FailureBreakdown.css';

/** Human labels for internal/providers/errors.go's three failure
 * categories. Fixed set, fixed meaning — not a generic categorical
 * series, so these get fixed colors rather than the provider palette. */
const CATEGORY_LABEL: Record<FailureCategory, string> = {
  key_fault: 'Key fault',
  provider_transient: 'Transient',
  bad_model: 'Bad model',
};

const CATEGORY_ORDER: FailureCategory[] = ['provider_transient', 'key_fault', 'bad_model'];

interface FailureBreakdownProps {
  failures: Partial<Record<FailureCategory, number>>;
}

/** A compact horizontal stacked bar showing how a provider's failed
 * attempts split across categories. Renders nothing (not a zero-width
 * bar) when there are no failures to show. */
export function FailureBreakdown({ failures }: FailureBreakdownProps) {
  const total = CATEGORY_ORDER.reduce((sum, c) => sum + (failures[c] ?? 0), 0);
  if (total === 0) {
    return <span className="failure-breakdown__empty">no failures</span>;
  }

  return (
    <div className="failure-breakdown">
      <div className="failure-breakdown__bar">
        {CATEGORY_ORDER.map((cat) => {
          const value = failures[cat] ?? 0;
          if (value === 0) return null;
          const pct = (value / total) * 100;
          return (
            <span
              key={cat}
              className={`failure-breakdown__segment failure-breakdown__segment--${cat}`}
              style={{ width: `${pct}%` }}
              title={`${CATEGORY_LABEL[cat]}: ${formatCount(value)}`}
            />
          );
        })}
      </div>
      <span className="failure-breakdown__total">{formatCount(total)}</span>
    </div>
  );
}
