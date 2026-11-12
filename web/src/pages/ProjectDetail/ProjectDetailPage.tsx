import { useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { useFlushProjectCache } from '../../api/mutations';
import type { Project } from '../../api/types';
import { Badge } from '../../components/ui/Badge';
import { lifecycleTone } from '../../components/ui/badgeTone';
import { Button } from '../../components/ui/Button';
import { Card } from '../../components/ui/Card';
import { ErrorBanner } from '../../components/ui/ErrorBanner';
import { Skeleton } from '../../components/ui/Skeleton';
import { PageHeader } from '../../components/layout/PageHeader';
import { CredentialsPanel } from './CredentialsPanel';
import { GatewayKeysPanel } from './GatewayKeysPanel';
import { SettingsForm } from './SettingsForm';
import './ProjectDetailPage.css';

// --- SCREENSHOT MOCK DATA ---------------------------------------------
// Hardcoded fixture so this page renders with realistic data without a
// seeded database or a running gateway. Remove this block (and the
// useProject call it replaces below) before merging back to main.
const MOCK_PROJECT: Project = {
  id: 'proj_prod_7f2a',
  name: 'production',
  status: 'active',
  rate_limit_per_min: 6000,
  max_concurrent_streams: 128,
  token_budget: 5_000_000,
  created_at: '2026-08-02T10:15:00Z',
};
// --- END SCREENSHOT MOCK DATA ------------------------------------------

export function ProjectDetailPage() {
  const { projectId } = useParams<{ projectId: string }>();
  const project = MOCK_PROJECT;
  const isLoading = false;
  const isError = false;
  const error = null;
  const refetch = () => {};
  const flushCache = useFlushProjectCache(projectId ?? '');
  const [flushed, setFlushed] = useState(false);

  if (!projectId) return null;

  return (
    <div>
      <Link to="/projects" className="project-detail__back">
        ← Projects
      </Link>

      {isError && <ErrorBanner error={error} onRetry={refetch} />}
      {isLoading && <Skeleton height="40px" width="240px" />}

      {project && !isLoading && (
        <>
          <PageHeader
            title={project.name}
            description={`Created ${new Date(project.created_at).toLocaleDateString()}`}
            actions={
              <>
                <Badge tone={lifecycleTone(project.status)}>{project.status}</Badge>
                <Button
                  onClick={async () => {
                    await flushCache.mutateAsync();
                    setFlushed(true);
                    setTimeout(() => setFlushed(false), 2000);
                  }}
                  disabled={flushCache.isPending}
                >
                  {flushCache.isPending ? 'Flushing…' : flushed ? 'Cache flushed' : 'Flush response cache'}
                </Button>
              </>
            }
          />

          <Card title="Settings" className="project-detail__section">
            <SettingsForm project={project} />
          </Card>

          <div className="project-detail__section">
            <CredentialsPanel projectId={projectId} />
          </div>

          <div className="project-detail__section">
            <GatewayKeysPanel projectId={projectId} />
          </div>
        </>
      )}
    </div>
  );
}
