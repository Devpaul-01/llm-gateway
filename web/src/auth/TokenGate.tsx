import { useState, type FormEvent, type ReactNode } from 'react';
import { ApiError } from '../api/client';
import { api } from '../api/endpoints';
import { Button } from '../components/ui/Button';
import { useToken } from './TokenContext';
import './TokenGate.css';

/** Gates the app behind the admin token. The backend has no login
 * endpoint (internal/httpserver/adminauth.go compares a single static
 * bearer token) — so "signing in" here means verifying the pasted token
 * actually works, by calling a cheap, harmless admin route, then storing
 * it for the session. */
export function TokenGate({ children }: { children: ReactNode }) {
  const { token, setToken } = useToken();
  const [input, setInput] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [checking, setChecking] = useState(false);

  if (token) return <>{children}</>;

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    if (!input.trim()) return;
    setChecking(true);
    setError(null);
    try {
      await api.listProviderNames(input.trim());
      setToken(input.trim());
    } catch (err) {
      if (err instanceof ApiError && err.isAuthError) {
        setError('That token was rejected by the gateway.');
      } else {
        setError('Could not reach the gateway. Check that it is running and this console is proxied to it.');
      }
    } finally {
      setChecking(false);
    }
  }

  return (
    <div className="token-gate">
      <form className="token-gate__panel" onSubmit={handleSubmit}>
        <div className="token-gate__brand">
          <svg width="22" height="22" viewBox="0 0 32 32" fill="none" aria-hidden="true">
            <path
              d="M8 20 L13 11 L18 17 L24 9"
              stroke="var(--accent)"
              strokeWidth="3"
              strokeLinecap="round"
              strokeLinejoin="round"
              fill="none"
            />
            <circle cx="24" cy="9" r="2.2" fill="var(--accent)" />
          </svg>
          <span>Gateway Console</span>
        </div>
        <label className="token-gate__label" htmlFor="admin-token">
          Admin token
        </label>
        <input
          id="admin-token"
          type="password"
          className="token-gate__input"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          placeholder="Paste your ADMIN_TOKEN"
          autoFocus
          autoComplete="off"
          spellCheck={false}
        />
        <p className="token-gate__hint">
          The same value as the gateway's <code>ADMIN_TOKEN</code> environment variable. Kept only for
          this browser tab.
        </p>
        {error && <p className="token-gate__error">{error}</p>}
        <Button type="submit" variant="primary" disabled={checking || !input.trim()}>
          {checking ? 'Checking…' : 'Connect'}
        </Button>
      </form>
    </div>
  );
}
