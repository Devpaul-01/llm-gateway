import { useState, type FormEvent } from 'react';
import { Link } from 'react-router-dom';
import { useCreateProject } from '../../api/mutations';
import { useProjects } from '../../api/queries';
import { Badge, lifecycleTone } from '../../components/ui/Badge';
import { Button } from '../../components/ui/Button';
import { Card } from '../../components/ui/Card';
import { EmptyState } from '../../components/ui/EmptyState';
import { ErrorBanner } from '../../components/ui/ErrorBanner';
import { Field } from '../../components/ui/Field';
import { Input } from '../../components/ui/Input';
import { Modal } from '../../components/ui/Modal';
import { Skeleton } from '../../components/ui/Skeleton';
import { PageHeader } from '../../components/layout/PageHeader';
import { formatCount, formatDateTime } from '../../lib/format';
import '../../components/ui/Table.css';

export function ProjectsPage() {
  const { data: projects, isLoading, isError, error, refetch } = useProjects();
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
        <Card>
          <Skeleton height="180px" />
        </Card>
      )}

      {projects && !isLoading && (
        <Card>
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
            <table className="table">
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Status</th>
                  <th className="num">Rate limit / min</th>
                  <th className="num">Max concurrent streams</th>
                  <th className="num">Token budget</th>
                  <th>Created</th>
                </tr>
              </thead>
              <tbody>
                {projects.map((p) => (
                  <tr key={p.id}>
                    <td>
                      <Link to={`/projects/${p.id}`} className="table-link">
                        {p.name}
                      </Link>
                    </td>
                    <td>
                      <Badge tone={lifecycleTone(p.status)}>{p.status}</Badge>
                    </td>
                    <td className="num">{p.rate_limit_per_min != null ? formatCount(p.rate_limit_per_min) : 'default'}</td>
                    <td className="num">
                      {p.max_concurrent_streams != null ? formatCount(p.max_concurrent_streams) : 'default'}
                    </td>
                    <td className="num">{p.token_budget != null ? formatCount(p.token_budget) : 'default'}</td>
                    <td className="mono">{formatDateTime(p.created_at)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </Card>
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
