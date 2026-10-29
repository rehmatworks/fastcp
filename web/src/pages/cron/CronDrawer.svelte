<script lang="ts">
  import Drawer from '../../components/Drawer.svelte';
  import Button from '../../components/Button.svelte';
  import Input from '../../components/Input.svelte';
  import Select from '../../components/Select.svelte';
  import Switch from '../../components/Switch.svelte';
  import { api, errorMessage, isAbort, seg } from '../../lib/api';
  import { session } from '../../lib/session.svelte';
  import { cronPresets } from '../../lib/validate';
  import { dateTime } from '../../lib/format';
  import type { CronDescription, CronJob } from '../../lib/types';

  interface Props {
    open: boolean;
    job: CronJob | null;
    onclose: () => void;
    onsaved: (job: CronJob) => void;
  }
  let { open, job, onclose, onsaved }: Props = $props();

  let name = $state('');
  let expression = $state('');
  let command = $state('');
  let enabled = $state(true);
  let busy = $state(false);
  let error = $state<string | null>(null);
  let described = $state<CronDescription | null>(null);

  const CUSTOM = '__custom';
  const presetOptions = [...cronPresets, { value: CUSTOM, label: 'Custom schedule' }];
  let preset = $state(CUSTOM);

  // Resets the form when the drawer opens. It must not read the form's own
  // state, or every edit would re-run it and undo the edit.
  $effect(() => {
    if (!open) return;
    const initial = job?.expression ?? '*/5 * * * *';
    name = job?.name ?? '';
    expression = initial;
    command = job?.command ?? '';
    enabled = job?.enabled ?? true;
    preset = cronPresets.some((p) => p.value === initial) ? initial : CUSTOM;
    error = null;
    described = null;
  });

  $effect(() => {
    const expr = expression.trim();
    if (!open || !expr) {
      described = null;
      return;
    }
    const c = new AbortController();
    const t = setTimeout(async () => {
      try {
        described = await api.post<CronDescription>('/cron/describe', { expression: expr }, { signal: c.signal });
      } catch (err) {
        if (!isAbort(err)) described = null;
      }
    }, 250);
    return () => {
      clearTimeout(t);
      c.abort();
    };
  });

  async function submit(e: Event) {
    e.preventDefault();
    busy = true;
    error = null;
    const body = { name: name.trim(), expression: expression.trim(), command: command.trim(), enabled };
    try {
      const saved = job ? await api.put<CronJob>(`/cron/${seg(job.id)}`, body) : await api.post<CronJob>('/cron', body);
      onsaved(saved);
    } catch (err) {
      error = errorMessage(err);
    } finally {
      busy = false;
    }
  }
</script>

<Drawer {open} title={job ? 'Edit cron job' : 'New cron job'} description="Commands run as {session.user?.username} from your home folder." {busy} {onclose}>
  <form id="cron-form" class="stack" onsubmit={submit}>
    <Input label="Name" bind:value={name} placeholder="Laravel scheduler" maxlength={100} required />
    <Select
      label="Schedule"
      bind:value={preset}
      options={presetOptions}
      onchange={(v) => {
        if (v !== CUSTOM) expression = v;
      }}
    />
    <Input
      label="Cron expression"
      bind:value={expression}
      mono
      autocomplete="off"
      spellcheck={false}
      oninput={() => (preset = cronPresets.some((p) => p.value === expression.trim()) ? expression.trim() : CUSTOM)}
      error={described && !described.valid ? (described.message ?? 'Invalid schedule.') : null}
      hint="minute hour day-of-month month day-of-week, or a macro such as @daily."
    />
    {#if described?.valid}
      <div class="preview">
        <p><strong>{described.description}</strong></p>
        {#if described.next_runs?.length}
          <p class="muted small">Next: {described.next_runs.map((r) => dateTime(r)).join(' · ')}</p>
        {/if}
      </div>
    {/if}
    <Input
      label="Command"
      bind:value={command}
      multiline
      rows={3}
      mono
      placeholder="php ~/apps/example_com/artisan schedule:run"
      maxlength={2048}
      required
      hint="A single line. Output is discarded unless you redirect it to a file."
    />
    <Switch label="Enabled" bind:checked={enabled} description="Disabled jobs are kept but do not run." />
    {#if error}<p class="notice strong" role="alert">{error}</p>{/if}
  </form>
  {#snippet footer()}
    <Button onclick={onclose} disabled={busy}>Cancel</Button>
    <Button
      type="submit"
      form="cron-form"
      variant="primary"
      loading={busy}
      disabled={!name.trim() || !command.trim() || described?.valid === false || command.includes('\n')}
    >
      {job ? 'Save changes' : 'Create cron job'}
    </Button>
  {/snippet}
</Drawer>

<style>
  .preview {
    padding: 10px 12px;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--bg-subtle);
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
</style>
