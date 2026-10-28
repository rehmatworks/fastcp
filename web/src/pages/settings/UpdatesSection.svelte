<script lang="ts">
  import RefreshCw from '@lucide/svelte/icons/refresh-cw';
  import ArrowUpCircle from '@lucide/svelte/icons/circle-arrow-up';
  import ExternalLink from '@lucide/svelte/icons/external-link';
  import Card from '../../components/Card.svelte';
  import Button from '../../components/Button.svelte';
  import Status from '../../components/Status.svelte';
  import Skeleton from '../../components/Skeleton.svelte';
  import ErrorState from '../../components/ErrorState.svelte';
  import ConfirmDialog from '../../components/ConfirmDialog.svelte';
  import { api, errorMessage, isAbort } from '../../lib/api';
  import { Resource, lifetime } from '../../lib/page.svelte';
  import { sleep } from '../../lib/jobs';
  import { toast } from '../../lib/toast.svelte';
  import { dateTime, relative } from '../../lib/format';
  import { agentJobView } from '../../lib/status';
  import type { AgentJob, UpdatesResponse } from '../../lib/types';

  const signal = lifetime();
  const updates = new Resource((sig) => api.get<UpdatesResponse>('/settings/updates', { signal: sig }));

  let checking = $state(false);
  let confirming = $state(false);
  let applying = $state<{ target: string; job: AgentJob | null; phase: string } | null>(null);

  const strip = (v: string | undefined) => (v ?? '').replace(/^v/, '');

  async function check() {
    checking = true;
    try {
      updates.set(await api.get<UpdatesResponse>('/settings/updates?refresh=1', { signal }));
    } catch (err) {
      if (!isAbort(err)) toast.error(errorMessage(err));
    } finally {
      checking = false;
    }
  }

  /** Starts the update, then waits for the new version to answer and reloads the page. */
  async function apply(version: string) {
    const job = await api.post<AgentJob>('/settings/updates/apply', { version });
    applying = { target: version, job, phase: 'Downloading and verifying the release…' };
    void watch(version);
  }

  async function watch(target: string) {
    const deadline = Date.now() + 15 * 60_000;
    let restarting = false;
    try {
      while (Date.now() < deadline) {
        await sleep(restarting ? 2000 : 3000, signal);
        try {
          const v = await api.get<{ version: string }>('/version', { signal, anonymous: true });
          if (strip(v.version) === strip(target)) {
            applying = { target, job: applying?.job ?? null, phase: 'Update installed. Reloading…' };
            await sleep(1500, signal);
            location.reload();
            return;
          }
          const st = await api.get<UpdatesResponse>('/settings/updates', { signal });
          if (st.job && applying) applying = { ...applying, job: st.job };
          if (st.job?.status === 'failed') {
            toast.error(st.job.message || 'The update failed. The previous version is still running.');
            applying = null;
            updates.set(st);
            return;
          }
        } catch (err) {
          if (isAbort(err)) throw err;
          if (!restarting && applying) applying = { ...applying, phase: 'Restarting FastCP…' };
          restarting = true;
        }
      }
      toast.error('The update is taking longer than expected. Check the server, then reload this page.');
      applying = null;
    } catch (err) {
      if (!isAbort(err)) toast.error(errorMessage(err));
    }
  }

  // Pick up an update that is already running, for example after a reload.
  let resumed = false;
  $effect(() => {
    const job = updates.data?.job;
    if (!job || resumed) return;
    resumed = true;
    if ((job.status === 'running' || job.status === 'queued') && job.target) {
      applying = { target: job.target, job, phase: job.message || 'Updating…' };
      void watch(job.target);
    }
  });
</script>

{#if updates.loading}
  <Skeleton variant="card" rows={1} />
{:else if updates.error || !updates.data}
  <ErrorState message={updates.error ?? 'Unknown error'} onretry={() => updates.reload()} />
{:else}
  {@const info = updates.data.info}
  {@const last = updates.data.job}
  <div class="stack">
    <Card title="FastCP">
      {#snippet actions()}
        <Button size="sm" icon={RefreshCw} loading={checking} disabled={!!applying} onclick={check}>Check now</Button>
      {/snippet}
      <dl class="kv">
        <dt>Installed version</dt>
        <dd class="mono">{info.current_version}</dd>
        {#if updates.data.agent_version && strip(updates.data.agent_version) !== strip(info.current_version)}
          <dt>Agent version</dt>
          <dd class="mono">{updates.data.agent_version}</dd>
        {/if}
        <dt>Latest release</dt>
        <dd>
          {#if info.latest_version}
            <span class="mono">{info.latest_version}</span>
            {#if info.release_notes_url}
              <a class="notes" href={info.release_notes_url} target="_blank" rel="noopener noreferrer">
                Release notes <ExternalLink size={12} strokeWidth={1.75} aria-hidden="true" />
              </a>
            {/if}
          {:else}
            <span class="muted">Unknown</span>
          {/if}
        </dd>
        <dt>Last checked</dt>
        <dd title={dateTime(info.checked_at)}>{relative(info.checked_at)}</dd>
      </dl>
      {#if info.error}<p class="notice strong check-error">{info.error}</p>{/if}
      {#snippet footer()}
        <div class="update-row">
          {#if applying}
            <Status kind="running" label={applying.phase} />
          {:else if info.update_available && info.latest_version}
            <span class="grow">FastCP {info.latest_version} is available.</span>
            <Button variant="primary" icon={ArrowUpCircle} onclick={() => (confirming = true)}>Update to {info.latest_version}</Button>
          {:else if !info.error}
            <Status kind="ok" label="FastCP is up to date" />
          {/if}
        </div>
      {/snippet}
    </Card>

    {#if last && !applying}
      {@const view = agentJobView(last.status)}
      <Card title="Last update">
        <dl class="kv">
          <dt>Version</dt>
          <dd class="mono">{last.target ?? '—'}</dd>
          <dt>Result</dt>
          <dd><Status kind={view.kind} label={view.label} /></dd>
          <dt>Finished</dt>
          <dd>{last.finished_at ? dateTime(last.finished_at) : '—'}</dd>
          {#if last.message}
            <dt>Details</dt>
            <dd class="details">{last.message}</dd>
          {/if}
        </dl>
      </Card>
    {/if}

    <p class="muted small">
      Releases are verified with FastCP's signing key before they are installed. If the new version does not start, the previous one is
      restored automatically.
    </p>
  </div>

  <ConfirmDialog
    open={confirming}
    title="Update to {info.latest_version}?"
    message="FastCP downloads the release, verifies its signature and restarts. Websites keep running; the panel is unavailable for up to a minute."
    actionLabel="Update now"
    destructive={false}
    onclose={() => (confirming = false)}
    onconfirm={() => apply(info.latest_version!)}
  />
{/if}

<style>
  .notes {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    margin-left: 10px;
    font-size: 12.5px;
    color: var(--fg-muted);
  }
  .check-error {
    margin-top: 12px;
  }
  .update-row {
    display: flex;
    align-items: center;
    gap: 12px;
    width: 100%;
    flex-wrap: wrap;
  }
  .grow {
    flex: 1;
  }
  .details {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
</style>
