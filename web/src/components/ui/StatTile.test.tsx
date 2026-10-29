import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { DASH, formatMs } from '../../lib/format';
import { StatTile } from './StatTile';

describe('StatTile', () => {
  it('renders a missing value as the dash sentinel with neutral tone, even when a status tone was requested', () => {
    render(<StatTile label="p99 latency" value={formatMs(null)} tone="bad" />);
    const value = screen.getByText(DASH);
    expect(value.className).toContain('stat-tile__value--neutral');
    expect(value.className).not.toContain('stat-tile__value--bad');
  });

  it('applies the requested tone to a real value', () => {
    render(<StatTile label="success rate" value="12%" tone="bad" />);
    const value = screen.getByText('12%');
    expect(value.className).toContain('stat-tile__value--bad');
  });
});
