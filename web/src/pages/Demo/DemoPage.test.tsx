import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Report } from '../../api/types';
import { DemoPage } from './DemoPage';
import { RECORDED_RUN } from './recordedRun';

function renderDemoPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <DemoPage />
    </QueryClientProvider>,
  );
}

const LIVE_REPORT: Report = {
  ...RECORDED_RUN,
  reliability: { ...RECORDED_RUN.reliability, requests: 999 },
};

describe('DemoPage', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('shows the live badge and live numbers when /public/metrics succeeds', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(new Response(JSON.stringify(LIVE_REPORT), { status: 200 })),
    );

    renderDemoPage();

    await waitFor(() => expect(screen.getByText(/live — real-time/i)).toBeInTheDocument());
    expect(screen.getByText('999')).toBeInTheDocument();
  });

  it('falls back to the recorded run and labels it as such when /public/metrics is unreachable', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('Failed to fetch')));

    renderDemoPage();

    await waitFor(() => expect(screen.getByText(/recorded run/i)).toBeInTheDocument());
    // The recorded fixture's headline request count should be on screen,
    // not a live count that can't exist without a reachable gateway.
    expect(screen.getByText(RECORDED_RUN.reliability.requests.toLocaleString())).toBeInTheDocument();
    expect(screen.getByText(/reproduce it yourself/i)).toBeInTheDocument();
  });

  it('falls back to the recorded run when /public/metrics returns an empty report (feature disabled, no traffic yet)', async () => {
    const empty: Report = { ...RECORDED_RUN, reliability: { ...RECORDED_RUN.reliability, requests: 0 } };
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify(empty), { status: 200 })));

    renderDemoPage();

    await waitFor(() => expect(screen.getByText(/recorded run/i)).toBeInTheDocument());
  });
});
