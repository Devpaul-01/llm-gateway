import { Link } from 'react-router-dom';
import type { Project } from '../../api/types';
import { Badge } from './Badge';
import { lifecycleTone } from './badgeTone';
import { formatCount, formatDateTime } from '../../lib/format';
import './ProjectCard.css';

interface ProjectCardProps {
  project: Project;
}

/** A project as a single tappable surface: the whole card is the link
 * target, not just the name text inside it. Table rows read as
 * unclickable on both desktop (only the name has a hover/click affordance)
 * and mobile (no hover state to discover it, and the row's hit target is
 * a thin line of text) — a card makes the entire area, including the
 * empty space around the stats, part of the same link. */
export function ProjectCard({ project }: ProjectCardProps) {
  return (
    <Link to={`/projects/${project.id}`} className="project-card">
      <div className="project-card__header">
        <span className="project-card__name">{project.name}</span>
        <Badge tone={lifecycleTone(project.status)}>{project.status}</Badge>
      </div>
      <dl className="project-card__stats">
        <div className="project-card__stat">
          <dt>Rate limit / min</dt>
          <dd>{project.rate_limit_per_min != null ? formatCount(project.rate_limit_per_min) : 'default'}</dd>
        </div>
        <div className="project-card__stat">
          <dt>Max concurrent streams</dt>
          <dd>{project.max_concurrent_streams != null ? formatCount(project.max_concurrent_streams) : 'default'}</dd>
        </div>
        <div className="project-card__stat">
          <dt>Token budget</dt>
          <dd>{project.token_budget != null ? formatCount(project.token_budget) : 'default'}</dd>
        </div>
      </dl>
      <div className="project-card__footer">
        <span className="mono">Created {formatDateTime(project.created_at)}</span>
        <span className="project-card__arrow" aria-hidden="true">
          →
        </span>
      </div>
    </Link>
  );
}
