import { createContext } from 'react';

export interface TokenContextValue {
  token: string | null;
  setToken: (token: string) => void;
  clearToken: () => void;
}

export const TokenContext = createContext<TokenContextValue | null>(null);
