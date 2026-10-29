import { describe, expect, it } from 'vitest';
import { DASH, formatCount, formatMs, formatMultiplier, formatPct } from './format';

describe('formatMs', () => {
  it('renders null as the dash sentinel, never 0', () => {
    expect(formatMs(null)).toBe(DASH);
    expect(formatMs(undefined)).toBe(DASH);
  });

  it('renders a real zero distinctly from null', () => {
    expect(formatMs(0)).toBe('0ms');
  });

  it('formats sub-second values in ms', () => {
    expect(formatMs(412)).toBe('412ms');
  });

  it('formats values at or over a second in seconds', () => {
    expect(formatMs(1500)).toBe('1.50s');
  });
});

describe('formatPct', () => {
  it('renders null as the dash sentinel', () => {
    expect(formatPct(null)).toBe(DASH);
  });

  it('renders a real zero rate as 0.0%, not a dash', () => {
    expect(formatPct(0)).toBe('0.0%');
  });

  it('converts a 0-1 ratio to a percentage string', () => {
    expect(formatPct(0.9834)).toBe('98.3%');
  });
});

describe('formatCount', () => {
  it('renders null as the dash sentinel', () => {
    expect(formatCount(null)).toBe(DASH);
  });

  it('shows exact values under 1000', () => {
    expect(formatCount(842)).toBe('842');
  });

  it('abbreviates thousands and millions', () => {
    expect(formatCount(1234)).toBe('1.2k');
    expect(formatCount(2_500_000)).toBe('2.5M');
  });
});

describe('formatMultiplier', () => {
  it('renders null as the dash sentinel', () => {
    expect(formatMultiplier(null)).toBe(DASH);
  });

  it('formats a speedup factor', () => {
    expect(formatMultiplier(42.3)).toBe('42.3×');
  });
});
