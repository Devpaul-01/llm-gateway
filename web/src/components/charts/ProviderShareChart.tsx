import { Bar, BarChart, Cell, LabelList, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';
import type { ProviderRow } from '../../api/types';
import { formatCount, formatPct } from '../../lib/format';
import { CATEGORICAL_SERIES_CAP, cssVar } from './chartTheme';
import './ChartTooltip.css';
import './chart-empty.css';

interface ProviderShareChartProps {
  providers: ProviderRow[];
}

/** Part-to-whole share of traffic by provider: horizontal bar, categorical
 * palette. Past the 8-slot validated categorical ceiling, the tail folds
 * into "Other" rather than generating a 9th hue (dataviz skill,
 * series-count ladder) — the gateway supports 12 providers, so this cap
 * is a real, reachable case, not a hypothetical. */
export function ProviderShareChart({ providers }: ProviderShareChartProps) {
  const grid = cssVar('--border');
  const axisText = cssVar('--text-faint');
  const otherColor = cssVar('--chart-other');

  const sorted = [...providers].filter((p) => p.requests > 0).sort((a, b) => b.requests - a.requests);
  const head = sorted.slice(0, CATEGORICAL_SERIES_CAP);
  const tail = sorted.slice(CATEGORICAL_SERIES_CAP);

  const rows = head.map((p) => ({ provider: p.provider, requests: p.requests, share: p.share, isOther: false }));
  if (tail.length > 0) {
    rows.push({
      provider: `Other (${tail.length})`,
      requests: tail.reduce((sum, p) => sum + p.requests, 0),
      share: tail.reduce((sum, p) => sum + p.share, 0),
      isOther: true,
    });
  }

  if (rows.length === 0) {
    return <p className="chart-empty">No provider traffic in this window.</p>;
  }

  return (
    <ResponsiveContainer width="100%" height={Math.max(rows.length * 36, 120)}>
      <BarChart data={rows} layout="vertical" margin={{ top: 0, right: 48, bottom: 0, left: 0 }}>
        <XAxis type="number" hide />
        <YAxis
          type="category"
          dataKey="provider"
          stroke={grid}
          tick={{ fill: 'var(--text)', fontSize: 12, fontFamily: 'var(--font-mono)' }}
          tickLine={false}
          axisLine={false}
          width={110}
        />
        <Tooltip content={<ProviderTooltip />} cursor={{ fill: 'var(--surface-raised)' }} />
        <Bar dataKey="requests" radius={[0, 4, 4, 0]} maxBarSize={24} isAnimationActive={false}>
          {rows.map((row, i) => (
            <Cell key={row.provider} fill={row.isOther ? otherColor : `var(--chart-${(i % 8) + 1})`} />
          ))}
          <LabelList
            dataKey="share"
            position="right"
            formatter={(v: unknown) => formatPct(typeof v === 'number' ? v : null, 0)}
            style={{ fill: axisText, fontSize: 11, fontFamily: 'var(--font-mono)' }}
          />
        </Bar>
      </BarChart>
    </ResponsiveContainer>
  );
}

function ProviderTooltip({ active, payload }: { active?: boolean; payload?: Array<{ payload: { provider: string; requests: number; share: number } }> }) {
  if (!active || !payload?.length) return null;
  const row = payload[0].payload;
  return (
    <div className="chart-tooltip">
      <div className="chart-tooltip__time">{row.provider}</div>
      <div className="chart-tooltip__row">
        <span>{formatCount(row.requests)} requests · {formatPct(row.share, 1)}</span>
      </div>
    </div>
  );
}
