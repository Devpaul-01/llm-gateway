import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { describe, expect, it } from 'vitest';
import type { Project } from '../../api/types';
import { ProjectCard } from './ProjectCard';

const PROJECT: Project = {
  id: 'proj_123',
  name: 'production',
  status: 'active',
  rate_limit_per_min: 300,
  max_concurrent_streams: 40,
  token_budget: 5_000_000,
  created_at: '2026-10-01T12:00:00Z',
};

describe('ProjectCard', () => {
  it('makes the whole card a single link to the project, not just the name text', () => {
    render(
      <MemoryRouter>
        <ProjectCard project={PROJECT} />
      </MemoryRouter>,
    );

    // The outermost element rendered by the component must itself be the
    // anchor — a row where only the name is wrapped in <Link> leaves the
    // rest of the surface (stats, padding, footer) dead to clicks and taps.
    const link = screen.getByRole('link', { name: /production/ });
    expect(link).toHaveAttribute('href', '/projects/proj_123');
    expect(link.className).toContain('project-card');

    // Content that sits outside the old table-row's clickable name cell
    // (a stat value, the created date) must be inside that same anchor.
    expect(link).toHaveTextContent('300');
    expect(link).toHaveTextContent('Created');
  });

  it('shows "default" for settings the project has not overridden', () => {
    const withDefaults: Project = {
      ...PROJECT,
      rate_limit_per_min: null,
      max_concurrent_streams: null,
      token_budget: null,
    };
    render(
      <MemoryRouter>
        <ProjectCard project={withDefaults} />
      </MemoryRouter>,
    );

    expect(screen.getAllByText('default')).toHaveLength(3);
  });
});
