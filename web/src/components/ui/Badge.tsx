import './Badge.css';

export type BadgeTone = 'neutral' | 'good' | 'warn' | 'bad';

interface BadgeProps {
  children: string;
  tone?: BadgeTone;
}

export function Badge({ children, tone = 'neutral' }: BadgeProps) {
  return <span className={`badge badge--${tone}`}>{children}</span>;
}

/** Maps a request_logs status (internal/requestlog) to a badge tone. */
export function statusTone(status: string): BadgeTone {
  switch (status) {
    case 'success':
      return 'good';
    case 'canceled':
      return 'neutral';
    case 'error':
    case 'partial':
      return 'bad';
    default:
      return 'neutral';
  }
}

/** Maps a project/credential/key status column value to a badge tone. */
export function lifecycleTone(status: string): BadgeTone {
  switch (status) {
    case 'active':
      return 'good';
    case 'revoked':
    case 'disabled':
      return 'bad';
    default:
      return 'neutral';
  }
}
