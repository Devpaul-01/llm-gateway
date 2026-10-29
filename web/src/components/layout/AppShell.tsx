import type { ReactNode } from 'react';
import { Sidebar } from './Sidebar';
import './AppShell.css';

export function AppShell({ children }: { children: ReactNode }) {
  return (
    <div className="app-shell">
      <Sidebar />
      <main className="app-shell__content">{children}</main>
    </div>
  );
}
