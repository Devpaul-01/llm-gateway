import { useState, type FormEvent } from 'react';
import { useCreateProject } from '../../api/mutations';
import type { Project } from '../../api/types';
import { Button } from '../../components/ui/Button';
import { EmptyState } from '../../components/ui/EmptyState';
import { ErrorBanner } from '../../components/ui/ErrorBanner';
import { Field } from '../../components/ui/Field';
import { Input } from '../../components/ui/Input';
import { Modal } from '../../components/ui/Modal';
import { ProjectCard } from '../../components/ui/ProjectCard';
import { Skeleton } from '../../components/ui/Skeleton';
import { PageHeader } from '../../components/layout/PageHeader';

// --- SCREENSHOT MOCK DATA ---------------------------------------------
// Hardcoded fixture so this page renders with realistic data without a
// seeded database or a running gateway. Remove this block (and the
// useProjects call it replaces below) before merging back to main.
const MOCK_PROJECTS: Project[] = [
  {
    id: 'proj_prod_7f2a',
    name: 'production',
    status: 'active',
    rate_limit_per_min: 6000,
    max_concurrent_streams: 128,
    token_budget: 5_000_000,
    created_at: '2026-08-02T10:15:00Z',
  },
  {
    id: 'proj_staging_3c91',
    name: 'staging',
    status: 'active',
    rate_limit_per_min: 1200,
    max_concurrent_streams: 32,
    token_budget: null,
    created_at: '2026-08-14T09:05:00Z',
  },
  {
    id: 'proj_internal_tools_9b0d',
    name: 'internal-tools',
    status: 'active',
    rate_limit_per_min: null,
    max_concurrent_streams: 16,
    token_budget: 250_000,
    created_at: '2026-09-28T16:42:00Z',
  },
  {
    id: 'proj_legacy_a114',
    name: 'legacy-batch',
    status: 'suspended',
    rate_limit_per_min: 300,
    max_concurrent_streams: 4,
    token_budget: 50_000,
    created_at: '2026-05-11T08:20:00Z',
  },
];
// --- END SCREENSHOT MOCK DATA ------------------------------------------

export function ProjectsPage() {
  const projects = MOCK_PROJECTS;
  const isLoading = false;
  const isError = false;
  const error = null;
  const refetch = () => {};
  const [showCreate, setShowCreate] = useState(false);

  return (
    <div>
      <PageHeader
        title="Projects"
        description="Rate limits, concurrency, and token budgets per project. Each project has its own credentials and gateway keys."
        actions={
          <Button variant="primary" onClick={() => setShowCreate(true)}>
            New project
          </Button>
        }
      />

      {isError && <ErrorBanner error={error} onRetry={refetch} />}

      {isLoading && (
        <div className="project-cards">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} height="140px" />
          ))}
        </div>
      )}

      {projects && !isLoading && (
        <>
          {projects.length === 0 ? (
            <EmptyState
              title="No projects yet"
              description="Create a project to issue gateway keys and provider credentials for it."
              action={
                <Button variant="primary" onClick={() => setShowCreate(true)}>
                  New project
                </Button>
              }
            />
          ) : (
            <div className="project-cards">
              {projects.map((p) => (
                <ProjectCard key={p.id} project={p} />
              ))}
            </div>
          )}
        </>
      )}

      {showCreate && <CreateProjectModal onClose={() => setShowCreate(false)} />}
    </div>
  );
}

function CreateProjectModal({ onClose }: { onClose: () => void }) {
  const [name, setName] = useState('');
  const createProject = useCreateProject();

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    if (!name.trim()) return;
    await createProject.mutateAsync(name.trim());
    onClose();
  }

  return (
    <Modal title="New project" onClose={onClose}>
      <form onSubmit={handleSubmit} style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
        <Field label="Name">
          <Input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="e.g. production"
            autoFocus
            required
          />
        </Field>
        {createProject.isError && <ErrorBanner error={createProject.error} />}
        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8 }}>
          <Button type="button" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={createProject.isPending || !name.trim()}>
            {createProject.isPending ? 'Creating…' : 'Create project'}
          </Button>
        </div>
      </form>
    </Modal>
  );
}
