import { QueryCache, QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useMemo } from 'react';
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom';
import { ApiError } from './api/client';
import { AppShell } from './components/layout/AppShell';
import { TokenGate } from './auth/TokenGate';
import { TokenProvider, useToken } from './auth/TokenContext';
import { DashboardPage } from './pages/Dashboard/DashboardPage';
import { ProvidersPage } from './pages/Providers/ProvidersPage';
import { ProjectsPage } from './pages/Projects/ProjectsPage';
import { ProjectDetailPage } from './pages/ProjectDetail/ProjectDetailPage';
import { RequestsPage } from './pages/Requests/RequestsPage';

function AppRoutes() {
  const { clearToken } = useToken();

  // A single place to react to a rejected token: any query/mutation that
  // gets a 401 means the admin token this session is using no longer
  // works (revoked, wrong value, gateway restarted with a new one) —
  // drop back to the TokenGate rather than let every page handle it.
  const queryClient = useMemo(
    () =>
      new QueryClient({
        queryCache: new QueryCache({
          onError: (error) => {
            if (error instanceof ApiError && error.isAuthError) clearToken();
          },
        }),
        defaultOptions: {
          queries: { retry: 1 },
        },
      }),
    [clearToken],
  );

  return (
    <QueryClientProvider client={queryClient}>
      <TokenGate>
        <BrowserRouter>
          <AppShell>
            <Routes>
              <Route path="/" element={<DashboardPage />} />
              <Route path="/providers" element={<ProvidersPage />} />
              <Route path="/projects" element={<ProjectsPage />} />
              <Route path="/projects/:projectId" element={<ProjectDetailPage />} />
              <Route path="/requests" element={<RequestsPage />} />
              <Route path="*" element={<Navigate to="/" replace />} />
            </Routes>
          </AppShell>
        </BrowserRouter>
      </TokenGate>
    </QueryClientProvider>
  );
}

export default function App() {
  return (
    <TokenProvider>
      <AppRoutes />
    </TokenProvider>
  );
}
