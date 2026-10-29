import { ApiError } from '../../api/client';
import './ErrorBanner.css';

interface ErrorBannerProps {
  error: unknown;
  onRetry?: () => void;
}

/** Renders the backend's own error message when available (ApiError),
 * rather than a generic "something went wrong" — the interface states
 * what happened, plainly, per the writing guidance. */
export function ErrorBanner({ error, onRetry }: ErrorBannerProps) {
  const message = error instanceof ApiError ? error.message : 'The request failed.';
  return (
    <div className="error-banner" role="alert">
      <span>{message}</span>
      {onRetry && (
        <button className="error-banner__retry" onClick={onRetry}>
          Retry
        </button>
      )}
    </div>
  );
}
