import { useState, type FormEvent } from 'react';
import { useCreateGatewayKey, useRevokeGatewayKey } from '../../api/mutations';
import { useGatewayKeys } from '../../api/queries';
import { Badge, lifecycleTone } from '../../components/ui/Badge';
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
import './GatewayKeysPanel.css';

export function GatewayKeysPanel({ projectId }: { projectId: string }) {
  const { data: keys, isLoading, isError, error, refetch } = useGatewayKeys(projectId);
  const [showCreate, setShowCreate] = useState(false);
  const [revealedKey, setRevealedKey] = useState<string | null>(null);
  const revoke = useRevokeGatewayKey(projectId);

  return (
    <Card
      title="Gateway keys"
      action={
        <Button variant="primary" onClick={() => setShowCreate(true)}>
          New gateway key
        </Button>
      }
    >
      {isError && <ErrorBanner error={error} onRetry={refetch} />}
      {isLoading && <Skeleton height="80px" />}

      {keys && !isLoading && (
        <>
          {keys.length === 0 ? (
            <EmptyState
              title="No gateway keys"
              description="A gateway key is what a client sends as its Authorization: Bearer header to call /v1/chat/completions through this project."
            />
          ) : (
            <table className="table">
              <thead>
                <tr>
                  <th>Key</th>
                  <th>Label</th>
                  <th>Status</th>
                  <th>Last used</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {keys.map((k) => (
                  <tr key={k.id}>
                    <td className="mono">{k.key_prefix}…</td>
                    <td>{k.label || <span className="muted">—</span>}</td>
                    <td>
                      <Badge tone={lifecycleTone(k.status)}>{k.status}</Badge>
                    </td>
                    <td className="mono">{k.last_used_at ? formatDateTime(k.last_used_at) : '—'}</td>
                    <td>
                      {k.status === 'active' && (
                        <Button
                          variant="danger"
                          onClick={() => {
                            if (confirm(`Revoke the gateway key "${k.label || k.key_prefix}"? Any client using it will immediately lose access.`)) {
                              revoke.mutate(k.id);
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

      {showCreate && (
        <CreateGatewayKeyModal
          projectId={projectId}
          onClose={() => setShowCreate(false)}
          onCreated={(key) => {
            setShowCreate(false);
            setRevealedKey(key);
          }}
        />
      )}

      {revealedKey && <RevealKeyModal value={revealedKey} onClose={() => setRevealedKey(null)} />}
    </Card>
  );
}

function CreateGatewayKeyModal({
  projectId,
  onClose,
  onCreated,
}: {
  projectId: string;
  onClose: () => void;
  onCreated: (key: string) => void;
}) {
  const [label, setLabel] = useState('');
  const create = useCreateGatewayKey(projectId);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    const result = await create.mutateAsync(label.trim());
    onCreated(result.key);
  }

  return (
    <Modal title="New gateway key" onClose={onClose}>
      <form onSubmit={handleSubmit} style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
        <Field label="Label" hint="Optional, for your own reference">
          <Input value={label} onChange={(e) => setLabel(e.target.value)} placeholder="e.g. prod-server" autoFocus />
        </Field>
        {create.isError && <ErrorBanner error={create.error} />}
        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8 }}>
          <Button type="button" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={create.isPending}>
            {create.isPending ? 'Creating…' : 'Create key'}
          </Button>
        </div>
      </form>
    </Modal>
  );
}

/** The plaintext key is returned exactly once, in createGatewayKeyResponse
 * — the backend never returns it again (handleListGatewayKeys only ever
 * sends key_prefix). This view does not persist it anywhere beyond this
 * component's state, which is discarded the moment the modal closes. */
function RevealKeyModal({ value, onClose }: { value: string; onClose: () => void }) {
  const [copied, setCopied] = useState(false);

  async function handleCopy() {
    await navigator.clipboard.writeText(value);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  }

  return (
    <Modal title="Gateway key created" onClose={onClose}>
      <p className="reveal-key__warning">
        This is the only time this key will be shown. Copy it now — the console cannot display it again.
      </p>
      <div className="reveal-key__value">
        <code>{value}</code>
      </div>
      <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8 }}>
        <Button onClick={handleCopy}>{copied ? 'Copied' : 'Copy to clipboard'}</Button>
        <Button variant="primary" onClick={onClose}>
          Done
        </Button>
      </div>
    </Modal>
  );
}
