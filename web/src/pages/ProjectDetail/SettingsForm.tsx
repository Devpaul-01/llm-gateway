import { useEffect, useState, type FormEvent } from 'react';
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

export function SettingsForm({ project }: SettingsFormProps) {
  const [rateLimit, setRateLimit] = useState('');
  const [concurrency, setConcurrency] = useState('');
  const [tokenBudget, setTokenBudget] = useState('');
  const [dirty, setDirty] = useState(false);
  const mutation = useUpdateProjectSettings(project.id);

  useEffect(() => {
    setRateLimit(project.rate_limit_per_min != null ? String(project.rate_limit_per_min) : '');
    setConcurrency(project.max_concurrent_streams != null ? String(project.max_concurrent_streams) : '');
    setTokenBudget(project.token_budget != null ? String(project.token_budget) : '');
    setDirty(false);
  }, [project.id, project.rate_limit_per_min, project.max_concurrent_streams, project.token_budget]);

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
