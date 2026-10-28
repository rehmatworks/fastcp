<script lang="ts">
  import Archive from '@lucide/svelte/icons/archive';
  import RotateCcw from '@lucide/svelte/icons/rotate-ccw';
  import Card from '../../components/Card.svelte';
  import Button from '../../components/Button.svelte';
  import Skeleton from '../../components/Skeleton.svelte';
  import ErrorState from '../../components/ErrorState.svelte';
  import EmptyState from '../../components/EmptyState.svelte';
  import ConfirmDialog from '../../components/ConfirmDialog.svelte';
  import Status from '../../components/Status.svelte';
  import { api, errorMessage } from '../../lib/api';
  import { Resource, lifetime } from '../../lib/page.svelte';
  import { pollJob } from '../../lib/jobs';
  import { toast } from '../../lib/toast.svelte';
  import { bytes, dateTime, relative } from '../../lib/format';
  import type { BackupJob, BackupsResponse, Site, Snapshot } from '../../lib/types';

  let { site }: { site: Site } = $props();
  const signal = lifetime();

  const data = new Resource(async (sig) => {
    const cfg = await api.get<BackupsResponse>('/backups', { signal: sig });
    if (!cfg.configured) return { cfg, snapshots: [] as Snapshot[] };
    const snapshots = await api.get<Snapshot[]>('/backups/snapshots?limit=200', { signal: sig });
    return { cfg, snapshots };
  });

  const names = $derived(new Set(site.domains.map((d) => d.domain)));
  const matching = $derived(
    (data.data?.snapshots ?? []).filter((s) => s.site_ids.includes(site.id) || s.sites.some((d) => names.has(d))),
  );

  let restoring = $state<Snapshot | null>(null);
  let running = $state<BackupJob | null>(null);

  async function restore(sn: Snapshot) {
    const job = await api.post<BackupJob>(`/backups/snapshots/${encodeURIComponent(sn.id)}/restore`, { type: 'site', site_id: site.id });
    running = job;
    toast.info('Restoring files. This can take a few minutes.');
    void pollJob((sig) => api.get<BackupJob>(`/backups/jobs/${job.id}`, { signal: sig }), {
      signal,
      onUpdate: (j) => (running = j),
    })
      .then((j) => {
        if (j.status === 'success') toast.success(`${site.domain} was restored from ${dateTime(sn.time)}.`);
        else toast.error(j.message || 'The restore failed.');
      })
      .catch((err) => {
        if (!signal.aborted) toast.error(errorMessage(err));
      })
      .finally(() => (running = null));
  }
</script>

{#if data.loading}
  <Skeleton rows={3} />
{:else if data.error}
  <ErrorState message={data.error} onretry={() => data.reload()} />
{:else if data.data && !data.data.cfg.configured}
  <EmptyState icon={Archive} title="Backups are not set up" description="Set a backup destination to keep restorable snapshots of this site.">
    <Button variant="primary" href="/backups/destination">Set up backups</Button>
  </EmptyState>
{:else}
  <Card
    title="Restore points"
    description="Snapshots that include this site. Restoring replaces the site’s files; logs and temporary files are kept."
    flush
  >
    {#snippet actions()}
      <Button size="sm" variant="ghost" href="/backups">All backups</Button>
    {/snippet}
    {#if running}
      <div class="running"><Status kind="running" label={running.message || 'Restoring…'} /></div>
    {/if}
    {#if matching.length === 0}
      <p class="muted pad">No snapshot includes this site yet. Run a backup from the Backups page.</p>
    {:else}
      <table class="table">
        <thead>
          <tr>
            <th>Snapshot</th>
            <th>Size</th>
            <th class="actions"><span class="sr-only">Actions</span></th>
          </tr>
        </thead>
        <tbody>
          {#each matching as sn (sn.id)}
            <tr>
              <td class="primary-cell">
                <div class="cell-title">{dateTime(sn.time)}</div>
                <div class="cell-sub">{relative(sn.time)} · <span class="mono">{sn.short_id}</span></div>
              </td>
              <td data-label="Size">{bytes(sn.bytes)}</td>
              <td class="actions">
                <Button size="sm" icon={RotateCcw} disabled={running !== null} onclick={() => (restoring = sn)}>Restore</Button>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </Card>
{/if}

<ConfirmDialog
  open={restoring !== null}
  title="Restore {site.domain}?"
  message="Files in the site folder are replaced with the copy from {restoring ? dateTime(restoring.time) : ''}. Files added since then are deleted. Databases are not changed."
  confirmText={site.domain}
  actionLabel="Restore files"
  onclose={() => (restoring = null)}
  onconfirm={() => (restoring ? restore(restoring) : undefined)}
/>

<style>
  .pad {
    padding: 4px 18px 18px;
  }
  .running {
    padding: 0 18px 12px;
  }
  .table th:first-child,
  .table td:first-child {
    padding-left: 18px;
  }
</style>
