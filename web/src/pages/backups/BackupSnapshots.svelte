<script lang="ts">
  import Archive from '@lucide/svelte/icons/archive';
  import RotateCcw from '@lucide/svelte/icons/rotate-ccw';
  import Download from '@lucide/svelte/icons/download';
  import Trash2 from '@lucide/svelte/icons/trash-2';
  import Button from '../../components/Button.svelte';
  import Skeleton from '../../components/Skeleton.svelte';
  import ErrorState from '../../components/ErrorState.svelte';
  import EmptyState from '../../components/EmptyState.svelte';
  import Menu from '../../components/Menu.svelte';
  import Status from '../../components/Status.svelte';
  import ConfirmDialog from '../../components/ConfirmDialog.svelte';
  import RestoreDialog from './RestoreDialog.svelte';
  import { api, errorMessage } from '../../lib/api';
  import { Resource } from '../../lib/page.svelte';
  import { toast } from '../../lib/toast.svelte';
  import { bytes, dateTime, relative, plural } from '../../lib/format';
  import type { BackupJob, BackupsResponse, Snapshot } from '../../lib/types';

  interface Props {
    data: BackupsResponse;
    revision: number;
    jobs: Record<string, BackupJob>;
    track: (job: BackupJob, opts?: { download?: boolean }) => void;
  }
  let { data, revision, jobs, track }: Props = $props();

  const snaps = new Resource((signal) =>
    data.configured ? api.get<Snapshot[]>('/backups/snapshots?limit=500', { signal }) : Promise.resolve([] as Snapshot[]),
  );

  let seen = 0;
  $effect(() => {
    if (revision !== seen) {
      seen = revision;
      if (revision > 0) void snaps.reload({ quiet: true });
    }
  });

  let restoring = $state<Snapshot | null>(null);
  let deleting = $state<Snapshot | null>(null);

  const busySnapshots = $derived(new Set(Object.values(jobs).map((j) => j.snapshot_id).filter(Boolean)));

  async function exportSnapshot(sn: Snapshot) {
    try {
      const job = await api.post<BackupJob>(`/backups/snapshots/${encodeURIComponent(sn.id)}/export`);
      toast.info('Preparing the download. It starts automatically when ready; you can also find it under Activity.');
      track(job, { download: true });
    } catch (err) {
      toast.error(errorMessage(err));
    }
  }

  function contents(sn: Snapshot): string {
    const parts = [];
    if (sn.sites.length) parts.push(plural(sn.sites.length, 'site'));
    if (sn.databases.length) parts.push(plural(sn.databases.length, 'database'));
    return parts.join(', ') || 'Empty';
  }
</script>

{#if !data.configured}
  <EmptyState icon={Archive} title="Backups are not set up" description="Choose where snapshots are stored, then run your first backup.">
    <Button variant="primary" href="/backups/destination">Set up backups</Button>
  </EmptyState>
{:else if snaps.loading}
  <Skeleton rows={4} />
{:else if snaps.error}
  <ErrorState message={snaps.error} onretry={() => snaps.reload()} />
{:else if (snaps.data?.length ?? 0) === 0}
  <EmptyState icon={Archive} title="No snapshots yet" description="Run a backup now, or wait for the next scheduled one." />
{:else}
  <div class="table-wrap">
    <table class="table">
      <thead>
        <tr>
          <th>Snapshot</th>
          <th>Contents</th>
          <th>Size</th>
          <th class="actions"><span class="sr-only">Actions</span></th>
        </tr>
      </thead>
      <tbody>
        {#each snaps.data ?? [] as sn (sn.id)}
          {@const busy = busySnapshots.has(sn.id)}
          <tr>
            <td class="primary-cell">
              <div class="cell-title">{dateTime(sn.time)}</div>
              <div class="cell-sub">{relative(sn.time)} · <span class="mono">{sn.short_id}</span></div>
            </td>
            <td data-label="Contents">
              <span title={[...sn.sites, ...sn.databases].join('\n')}>{contents(sn)}</span>
            </td>
            <td data-label="Size" class="nowrap">
              {bytes(sn.bytes)}
              {#if sn.data_added !== undefined}<div class="cell-sub">+{bytes(sn.data_added)} new</div>{/if}
            </td>
            <td class="actions">
              {#if busy}
                <Status kind="running" label="Working…" />
              {:else}
                <div class="row nowrap">
                  <Button size="sm" icon={RotateCcw} onclick={() => (restoring = sn)}>Restore</Button>
                  <Menu
                    label="More actions for snapshot {sn.short_id}"
                    items={[
                      { label: 'Download as zip', icon: Download, onclick: () => exportSnapshot(sn) },
                      { label: 'Delete snapshot', icon: Trash2, destructive: true, onclick: () => (deleting = sn) },
                    ]}
                  />
                </div>
              {/if}
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}

<RestoreDialog
  snapshot={restoring}
  {data}
  onclose={() => (restoring = null)}
  onstarted={(job) => {
    toast.info('Restore started.');
    track(job);
  }}
/>

<ConfirmDialog
  open={deleting !== null}
  title="Delete snapshot {deleting?.short_id}?"
  message="The snapshot from {deleting ? dateTime(deleting.time) : ''} is removed from the repository. Data shared with other snapshots is kept."
  actionLabel="Delete snapshot"
  onclose={() => (deleting = null)}
  onconfirm={async () => {
    if (!deleting) return;
    const job = await api.del<BackupJob>(`/backups/snapshots/${encodeURIComponent(deleting.id)}`);
    track(job);
  }}
/>
