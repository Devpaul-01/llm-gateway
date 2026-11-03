import { useCallback, useMemo, useState, type ReactNode } from 'react';
import { TokenContext } from './tokenContextStore';

const STORAGE_KEY = 'gateway-console.admin-token';

/** Holds the admin bearer token for the session. Kept in sessionStorage
 * (cleared when the tab closes) rather than localStorage, to limit how
 * long a token persists on a shared machine — see
 * docs/FRONTEND_ARCHITECTURE.md §8. Never put in a URL or query string. */
export function TokenProvider({ children }: { children: ReactNode }) {
  const [token, setTokenState] = useState<string | null>(() => sessionStorage.getItem(STORAGE_KEY));

  const setToken = useCallback((next: string) => {
    sessionStorage.setItem(STORAGE_KEY, next);
    setTokenState(next);
  }, []);

  const clearToken = useCallback(() => {
    sessionStorage.removeItem(STORAGE_KEY);
    setTokenState(null);
  }, []);

  const value = useMemo(() => ({ token, setToken, clearToken }), [token, setToken, clearToken]);

  return <TokenContext.Provider value={value}>{children}</TokenContext.Provider>;
}
