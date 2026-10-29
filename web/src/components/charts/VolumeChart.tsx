import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';
import type { TimeseriesPoint } from '../../api/types';
import { formatCount, formatDateTime } from '../../lib/format';
import { cssVar } from './chartTheme';
import './ChartTooltip.css';

interface VolumeChartProps {
  data: TimeseriesPoint[];
}

/** Request volume over time. A single series (total requests) is the
 * subject, so this gets a sequential treatment — one hue, area wash —
 * not a categorical palette: see dataviz skill, "trend over time". */
export function VolumeChart({ data }: VolumeChartProps) {
  const accent = cssVar('--accent');
  const grid = cssVar('--border');
  const axisText = cssVar('--text-faint');

  return (
    <ResponsiveContainer width="100%" height={200}>
      <AreaChart data={data} margin={{ top: 8, right: 8, bottom: 0, left: 0 }}>
        <defs>
          <linearGradient id="volumeFill" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor={accent} stopOpacity={0.22} />
            <stop offset="100%" stopColor={accent} stopOpacity={0.02} />
          </linearGradient>
        </defs>
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
          tickFormatter={(v: number) => formatCount(v)}
          width={44}
        />
        <Tooltip content={<VolumeTooltip />} />
        <Area
          type="monotone"
          dataKey="requests"
          stroke={accent}
          strokeWidth={2}
          fill="url(#volumeFill)"
          isAnimationActive={false}
        />
      </AreaChart>
    </ResponsiveContainer>
  );
}

function VolumeTooltip({ active, payload, label }: { active?: boolean; payload?: Array<{ value: number }>; label?: string }) {
  if (!active || !payload?.length || !label) return null;
  return (
    <div className="chart-tooltip">
      <div className="chart-tooltip__time">{formatDateTime(label)}</div>
      <div className="chart-tooltip__row">
        <span className="chart-tooltip__swatch" style={{ background: 'var(--accent)' }} />
        <span>{formatCount(payload[0].value)} requests</span>
      </div>
    </div>
  );
}
