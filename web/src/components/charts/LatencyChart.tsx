import { CartesianGrid, Legend, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';
import type { TimeseriesPoint } from '../../api/types';
import { DASH, formatDateTime, formatMs } from '../../lib/format';
import { cssVar } from './chartTheme';
import './ChartTooltip.css';

interface LatencyChartProps {
  data: TimeseriesPoint[];
}

/** p50 vs p95 over time: two series telling distinct stories (typical vs
 * tail latency), so this is categorical, not sequential — with a legend
 * since there are 2 series (dataviz skill: "a legend is always present
 * for two or more series"). */
export function LatencyChart({ data }: LatencyChartProps) {
  const p50Color = cssVar('--accent');
  const p95Color = cssVar('--chart-7');
  const grid = cssVar('--border');
  const axisText = cssVar('--text-faint');

  // Buckets with no successful (non-cache-hit) requests have p50_ms/p95_ms
  // == null server-side; Recharts simply breaks the line across a null
  // point rather than drawing a false zero, which is the correct behavior
  // here — a gap, not a dip to 0ms.
  return (
    <ResponsiveContainer width="100%" height={200}>
      <LineChart data={data} margin={{ top: 8, right: 8, bottom: 0, left: 0 }}>
        <CartesianGrid stroke={grid} strokeDasharray="0" vertical={false} />
        <XAxis
          dataKey="t"
          tickFormatter={(t: string) => formatDateTime(t)}
          stroke={grid}
          tick={{ fill: axisText, fontSize: 11, fontFamily: 'var(--font-mono)' }}
          tickLine={false}
          axisLine={{ stroke: grid }}
          minTickGap={48}
        />
        <YAxis
          stroke={grid}
          tick={{ fill: axisText, fontSize: 11, fontFamily: 'var(--font-mono)' }}
          tickLine={false}
          axisLine={false}
          tickFormatter={(v: number) => formatMs(v)}
          width={52}
        />
        <Tooltip content={<LatencyTooltip p50Color={p50Color} p95Color={p95Color} />} />
        <Legend
          verticalAlign="top"
          align="right"
          height={28}
          formatter={(value: string) => <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>{value}</span>}
        />
        <Line
          type="monotone"
          dataKey="p50_ms"
          name="p50"
          stroke={p50Color}
          strokeWidth={2}
          dot={false}
          connectNulls={false}
          isAnimationActive={false}
        />
        <Line
          type="monotone"
          dataKey="p95_ms"
          name="p95"
          stroke={p95Color}
          strokeWidth={2}
          dot={false}
          connectNulls={false}
          isAnimationActive={false}
        />
      </LineChart>
    </ResponsiveContainer>
  );
}

interface LatencyTooltipProps {
  active?: boolean;
  payload?: Array<{ value: number | null; dataKey: string }>;
  label?: string;
  p50Color: string;
  p95Color: string;
}

function LatencyTooltip({ active, payload, label, p50Color, p95Color }: LatencyTooltipProps) {
  if (!active || !payload?.length || !label) return null;
  const p50 = payload.find((p) => p.dataKey === 'p50_ms')?.value ?? null;
  const p95 = payload.find((p) => p.dataKey === 'p95_ms')?.value ?? null;
  return (
    <div className="chart-tooltip">
      <div className="chart-tooltip__time">{formatDateTime(label)}</div>
      <div className="chart-tooltip__row">
        <span className="chart-tooltip__swatch" style={{ background: p50Color }} />
        <span>p50 {p50 != null ? formatMs(p50) : DASH}</span>
      </div>
      <div className="chart-tooltip__row">
        <span className="chart-tooltip__swatch" style={{ background: p95Color }} />
        <span>p95 {p95 != null ? formatMs(p95) : DASH}</span>
      </div>
    </div>
  );
}
