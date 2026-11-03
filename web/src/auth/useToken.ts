import { useContext } from 'react';
import { TokenContext, type TokenContextValue } from './tokenContextStore';

export function useToken(): TokenContextValue {
  const ctx = useContext(TokenContext);
  if (!ctx) throw new Error('useToken must be used within a TokenProvider');
  return ctx;
}
