import { useState, type FormEvent } from 'react';
import { useCreateCredential, useRevokeCredential } from '../../api/mutations';
import type { CredentialMetadata } from '../../api/types';
import { Badge } from '../../components/ui/Badge';
import { lifecycleTone } from '../../components/ui/badgeTone';
import { Button } from '../../components/ui/Button';
import { Card } from '../../components/ui/Card';
import { EmptyState } from '../../components/ui/EmptyState';
import { ErrorBanner } from '../../components/ui/ErrorBanner';
import { Field } from '../../components/ui/Field';
import { Input } from '../../components/ui/Input';
import { Modal } from '../../components/ui/Modal';
import { Skeleton } from '../../components/ui/Skeleton';
import { formatDateTime } from '../../lib/format';
import '../../components/ui/Table.css';

// --- SCREENSHOT MOCK DATA ---------------------------------------------
// Hardcoded fixture so this panel renders with realistic data without a
// seeded database or a running gateway. Remove this block (and the
// useCredentials call it replaces below) before merging back to main.
const MOCK_CREDENTIALS: CredentialMetadata[] = [
  {
    id: 'cred_openai_441',
    project_id: 'proj_prod_7f2a',
    provider: 'openai',
    label: 'primary',
    status: 'active',
    created_at: '2026-08-02T10:20:00Z',
    last_used_at: '2026-11-12T17:59:41Z',
  },
  {
    id: 'cred_anthropic_118',
    project_id: 'proj_prod_7f2a',
    provider: 'anthropic',
    label: 'primary',
    status: 'active',
    created_at: '2026-08-02T10:21:00Z',
    last_used_at: '2026-11-12T17:59:22Z',
  },
  {
    id: 'cred_openai_old_055',
    project_id: 'proj_prod_7f2a',
    provider: 'openai',
    label: 'legacy-key',
    status: 'revoked',
    created_at: '2026-08-02T10:22:00Z',
    last_used_at: '2026-09-30T04:11:00Z',
  },
];
// --- END SCREENSHOT MOCK DATA ------------------------------------------

export function CredentialsPanel({ projectId }: { projectId: string }) {
  const credentials = MOCK_CREDENTIALS;
  const isLoading = false;
  const isError = false;
  const error = null;
  const refetch = () => {};
  const [showCreate, setShowCreate] = useState(false);
  const revoke = useRevokeCredential(projectId);

  return (
    <Card
      title="Provider credentials"
      action={
        <Button variant="primary" onClick={() => setShowCreate(true)}>
          Add credential
        </Button>
      }
    >
      {isError && <ErrorBanner error={error} onRetry={refetch} />}
      {isLoading && <Skeleton height="80px" />}

      {credentials && !isLoading && (
        <>
          {credentials.length === 0 ? (
            <EmptyState
              title="No provider credentials"
              description="Add an API key for at least one provider before issuing a gateway key — the gateway has nothing to route to otherwise."
            />
          ) : (
            <table className="table">
              <thead>
                <tr>
                  <th>Provider</th>
                  <th>Label</th>
                  <th>Status</th>
                  <th>Last used</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {credentials.map((c) => (
                  <tr key={c.id}>
                    <td className="mono">{c.provider}</td>
                    <td>{c.label || <span className="muted">—</span>}</td>
                    <td>
                      <Badge tone={lifecycleTone(c.status)}>{c.status}</Badge>
                    </td>
                    <td className="mono">{c.last_used_at ? formatDateTime(c.last_used_at) : '—'}</td>
                    <td>
                      {c.status === 'active' && (
                        <Button
                          variant="danger"
                          onClick={() => {
                            if (confirm(`Revoke the ${c.provider} credential "${c.label || c.id}"?`)) {
                              revoke.mutate(c.id);
                            }
                          }}
                          disabled={revoke.isPending}
                        >
                          Revoke
                        </Button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </>
      )}

      {showCreate && <CreateCredentialModal projectId={projectId} onClose={() => setShowCreate(false)} />}
    </Card>
  );
}

function CreateCredentialModal({ projectId, onClose }: { projectId: string; onClose: () => void }) {
  // SCREENSHOT MOCK DATA: hardcoded in place of useProviderNames().
  const providers = ['openai', 'anthropic', 'mock-primary', 'mock-secondary'];
  const [provider, setProvider] = useState('');
  const [label, setLabel] = useState('');
  const [apiKey, setApiKey] = useState('');
  const create = useCreateCredential(projectId);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    if (!provider || !apiKey.trim()) return;
    await create.mutateAsync({ provider, label: label.trim(), api_key: apiKey.trim() });
    onClose();
  }

  return (
    <Modal title="Add provider credential" onClose={onClose}>
      <form onSubmit={handleSubmit} style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
        <Field label="Provider">
          <select
            className="input"
            value={provider}
            onChange={(e) => setProvider(e.target.value)}
            required
            autoFocus
          >
            <option value="" disabled>
              Select a provider
            </option>
            {providers.map((p) => (
              <option key={p} value={p}>
                {p}
              </option>
            ))}
          </select>
        </Field>
        <Field label="Label" hint="Optional, for your own reference">
          <Input value={label} onChange={(e) => setLabel(e.target.value)} placeholder="e.g. primary" />
        </Field>
        <Field label="API key">
          <Input
            type="password"
            value={apiKey}
            onChange={(e) => setApiKey(e.target.value)}
            placeholder="Provider's API key"
            autoComplete="off"
            required
          />
        </Field>
        {create.isError && <ErrorBanner error={create.error} />}
        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8 }}>
          <Button type="button" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={create.isPending || !provider || !apiKey.trim()}>
            {create.isPending ? 'Adding…' : 'Add credential'}
          </Button>
        </div>
      </form>
    </Modal>
  );
}
