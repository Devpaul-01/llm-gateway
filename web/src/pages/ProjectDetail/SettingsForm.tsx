import { useState, type FormEvent } from 'react';
import { useUpdateProjectSettings } from '../../api/mutations';
import type { Project } from '../../api/types';
import { Button } from '../../components/ui/Button';
import { Field } from '../../components/ui/Field';
import { Input } from '../../components/ui/Input';
import { ErrorBanner } from '../../components/ui/ErrorBanner';
import './SettingsForm.css';

interface SettingsFormProps {
  project: Project;
}

/** Each setting is tri-state, matching internal/httpserver/admin.go's
 * `optionalInt`: leave it alone (the field is simply not sent), set it to
 * a number, or clear it back to the server default (send `null`
 * explicitly). The text input's own empty string stands for "clear" here
 * — there is no fourth state in the UI, so an empty field always means
 * "use the default," never "no opinion." */
function toUpdatePayload(raw: string): number | null | undefined {
  if (raw.trim() === '') return null;
  const n = Number(raw);
  if (!Number.isFinite(n) || n <= 0) return undefined; // invalid, don't send
  return Math.floor(n);
}

function fieldsFromProject(project: Project) {
  return {
    rateLimit: project.rate_limit_per_min != null ? String(project.rate_limit_per_min) : '',
    concurrency: project.max_concurrent_streams != null ? String(project.max_concurrent_streams) : '',
    tokenBudget: project.token_budget != null ? String(project.token_budget) : '',
  };
}

export function SettingsForm({ project }: SettingsFormProps) {
  // Tracks which project's settings are currently reflected in state, so a
  // changed (or freshly refetched) `project` can be synced during render --
  // React's documented alternative to an effect for "adjust state when a
  // prop changes" -- instead of a setState-in-effect render-then-render.
  const [syncedFrom, setSyncedFrom] = useState(project);
  const [rateLimit, setRateLimit] = useState(() => fieldsFromProject(project).rateLimit);
  const [concurrency, setConcurrency] = useState(() => fieldsFromProject(project).concurrency);
  const [tokenBudget, setTokenBudget] = useState(() => fieldsFromProject(project).tokenBudget);
  const [dirty, setDirty] = useState(false);
  const mutation = useUpdateProjectSettings(project.id);

  if (
    project.id !== syncedFrom.id ||
    project.rate_limit_per_min !== syncedFrom.rate_limit_per_min ||
    project.max_concurrent_streams !== syncedFrom.max_concurrent_streams ||
    project.token_budget !== syncedFrom.token_budget
  ) {
    const fields = fieldsFromProject(project);
    setSyncedFrom(project);
    setRateLimit(fields.rateLimit);
    setConcurrency(fields.concurrency);
    setTokenBudget(fields.tokenBudget);
    setDirty(false);
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    await mutation.mutateAsync({
      rate_limit_per_min: toUpdatePayload(rateLimit),
      max_concurrent_streams: toUpdatePayload(concurrency),
      token_budget: toUpdatePayload(tokenBudget),
    });
    setDirty(false);
  }

  return (
    <form className="settings-form" onSubmit={handleSubmit}>
      <div className="settings-form__grid">
        <Field label="Rate limit (requests / min)" hint="Blank uses the server default">
          <Input
            type="number"
            min={1}
            value={rateLimit}
            onChange={(e) => {
              setRateLimit(e.target.value);
              setDirty(true);
            }}
            placeholder="default"
          />
        </Field>
        <Field label="Max concurrent streams" hint="Blank uses the server default">
          <Input
            type="number"
            min={1}
            value={concurrency}
            onChange={(e) => {
              setConcurrency(e.target.value);
              setDirty(true);
            }}
            placeholder="default"
          />
        </Field>
        <Field label="Daily token budget" hint="Blank uses the server default">
          <Input
            type="number"
            min={1}
            value={tokenBudget}
            onChange={(e) => {
              setTokenBudget(e.target.value);
              setDirty(true);
            }}
            placeholder="default"
          />
        </Field>
      </div>

      {mutation.isError && <ErrorBanner error={mutation.error} />}
      {mutation.isSuccess && !dirty && <p className="settings-form__saved">Settings saved.</p>}

      <div className="settings-form__actions">
        <Button type="submit" variant="primary" disabled={!dirty || mutation.isPending}>
          {mutation.isPending ? 'Saving…' : 'Save settings'}
        </Button>
      </div>
    </form>
  );
}
