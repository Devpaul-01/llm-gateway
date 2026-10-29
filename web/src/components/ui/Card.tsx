import type { ReactNode } from 'react';
import './Card.css';

interface CardProps {
  title?: string;
  action?: ReactNode;
  children: ReactNode;
  className?: string;
}

/** A labeled panel. Not a rounded-shadow "card kit" box — differentiated
 * from the page background by a hairline border only. */
export function Card({ title, action, children, className }: CardProps) {
  return (
    <section className={`card${className ? ` ${className}` : ''}`}>
      {(title || action) && (
        <header className="card__header">
          {title && <h2 className="card__title">{title}</h2>}
          {action}
        </header>
      )}
      <div className="card__body">{children}</div>
    </section>
  );
}
