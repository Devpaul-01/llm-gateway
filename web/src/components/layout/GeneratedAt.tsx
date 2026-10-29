import { formatTimestamp } from '../../lib/format';
import './GeneratedAt.css';

export function GeneratedAt({ iso }: { iso: string | undefined }) {
  if (!iso) return null;
  return (
    <span className="generated-at" title={iso}>
      as of {formatTimestamp(iso)}
    </span>
  );
}
