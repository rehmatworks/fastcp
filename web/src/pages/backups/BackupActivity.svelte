<script lang="ts">
  import History from '@lucide/svelte/icons/history';
  import Download from '@lucide/svelte/icons/download';
  import Button from '../../components/Button.svelte';
  import Skeleton from '../../components/Skeleton.svelte';
  import ErrorState from '../../components/ErrorState.svelte';
  import EmptyState from '../../components/EmptyState.svelte';
  import Status from '../../components/Status.svelte';
  import { api, errorMessage } from '../../lib/api';
  import { Resource } from '../../lib/page.svelte';
  import { toast } from '../../lib/toast.svelte';
  import { downloadExport } from '../../lib/download';
  import { dateTime, elapsed, relative } from '../../lib/format';
  import { backupJobView, jobTypeLabels } from '../../lib/status';
  import type { BackupJob } from '../../lib/types';

  let { revision, jobs }: { revision: number; jobs: Record<string, BackupJob> } = $props();

  const list = new Resource((signal) => api.get<BackupJob[]>('/backups/jobs?limit=100', { signal }));

  let seen = 0;
  $effect(() => {
    if (revision !== seen) {
      seen = revision;
      if (revision > 0) void list.reload({ quiet: true });
    }
  });

  const rows = $derived((list.data ?? []).map((j) => jobs[j.id] ?? j));
  const anyRunning = $derived(rows.some((j) => j.status === 'running') || Object.keys(jobs).length > 0);

  $effect(() => {
    if (!anyRunning) return;
    const t = setInterval(() => void list.reload({ quiet: true }), 5000);
    return () => clearInterval(t);
  });

  let downloading = $state<string | null>(null);
  async function download(j: BackupJob) {
    downloading = j.id;
    try {
      await downloadExport(j.id);
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      downloading = null;
    }
  }
</script>

{#if list.loading}
  <Skeleton rows={5} />
{:else if list.error}
  <ErrorState message={list.error} onretry={() => list.reload()} />
{:else if rows.length === 0}
  <EmptyState icon={History} title="No activity yet" description="Backups, restores and downloads appear here." />
{:else}
  <div class="table-wrap">
    <table class="table">
      <thead>
        <tr>
          <th>Job</th>
          <th>Status</th>
          <th>Details</th>
          <th>Duration</th>
          <th class="actions"><span class="sr-only">Actions</span></th>
        </tr>
      </thead>
      <tbody>
        {#each rows as j (j.id)}
          {@const view = backupJobView(j.status)}
          <tr>
            <td class="primary-cell">
              <div class="cell-title">{jobTypeLabels[j.job_type] ?? j.job_type}</div>
              <div class="cell-sub" title={dateTime(j.started_at)}>{relative(j.started_at)}</div>
            </td>
            <td data-label="Status"><Status kind={view.kind} label={view.label} /></td>
            <td data-label="Details" class="details">{j.message || '—'}</td>
            <td data-label="Duration" class="nowrap">{elapsed(j.started_at, j.finished_at)}</td>
            <td class="actions">
              {#if j.job_type === 'export' && j.status === 'success' && j.artifact}
                <Button size="sm" icon={Download} loading={downloading === j.id} onclick={() => download(j)}>Download</Button>
              {/if}
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}

<style>
  .details {
    max-width: 420px;
    overflow-wrap: anywhere;
    color: var(--fg-muted);
  }
</style>
