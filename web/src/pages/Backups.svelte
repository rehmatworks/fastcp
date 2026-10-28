<script lang="ts">
  import Play from '@lucide/svelte/icons/play';
  import PageHeader from '../components/PageHeader.svelte';
  import Tabs from '../components/Tabs.svelte';
  import Button from '../components/Button.svelte';
  import Skeleton from '../components/Skeleton.svelte';
  import ErrorState from '../components/ErrorState.svelte';
  import Status from '../components/Status.svelte';
  import BackupSnapshots from './backups/BackupSnapshots.svelte';
  import BackupDestination from './backups/BackupDestination.svelte';
  import BackupActivity from './backups/BackupActivity.svelte';
  import { api, errorMessage } from '../lib/api';
  import { Resource, lifetime } from '../lib/page.svelte';
  import { pollJob } from '../lib/jobs';
  import { toast } from '../lib/toast.svelte';
  import { downloadExport } from '../lib/download';
  import { relative } from '../lib/format';
  import { backupJobView, backupRunTypes, jobTypeLabels } from '../lib/status';
  import type { BackupJob, BackupsResponse } from '../lib/types';

  let { params }: { params: { tab?: string } } = $props();
  const signal = lifetime();

  const cfg = new Resource((sig) => api.get<BackupsResponse>('/backups', { signal: sig }));
  /** Bumped whenever a job finishes so tabs reload their data. */
  let revision = $state(0);
  let jobs = $state<Record<string, BackupJob>>({});
  let starting = $state(false);

  const tabs = [
    { id: 'snapshots', label: 'Snapshots', href: '/backups' },
    { id: 'destination', label: 'Destination & schedule', href: '/backups/destination' },
    { id: 'activity', label: 'Activity', href: '/backups/activity' },
  ];
  const active = $derived(tabs.some((t) => t.id === params.tab) ? params.tab! : 'snapshots');
  const runningBackup = $derived(Object.values(jobs).find((j) => backupRunTypes.has(j.job_type) && j.status === 'running'));
  const last = $derived(cfg.data?.config);
  const lastView = $derived(last ? backupJobView(last.last_status) : null);

  /** Follows a job until it finishes, then refreshes the page data. */
  function track(job: BackupJob, opts: { download?: boolean } = {}) {
    jobs[job.id] = job;
    void pollJob((sig) => api.get<BackupJob>(`/backups/jobs/${job.id}`, { signal: sig }), {
      signal,
      onUpdate: (j) => (jobs[j.id] = j),
    })
      .then(async (j) => {
        const label = jobTypeLabels[j.job_type] ?? 'Job';
        if (j.status === 'success') {
          toast.success(j.message || `${label} finished.`);
          if (opts.download && j.job_type === 'export') {
            try {
              await downloadExport(j.id);
            } catch (err) {
              toast.error(errorMessage(err));
            }
          }
        } else {
          toast.error(j.message || `${label} failed.`);
        }
      })
      .catch((err) => {
        if (!signal.aborted) toast.error(errorMessage(err));
      })
      .finally(() => {
        delete jobs[job.id];
        revision++;
        void cfg.reload({ quiet: true });
      });
  }

  // Resume following a backup that was already running when the page opened.
  let resumed = false;
  $effect(() => {
    const id = cfg.data?.config.running_job_id;
    if (!id || resumed) return;
    resumed = true;
    api
      .get<BackupJob>(`/backups/jobs/${id}`, { signal })
      .then((j) => {
        if (j.status === 'running') track(j);
      })
      .catch(() => {});
  });

  async function runNow() {
    starting = true;
    try {
      const job = await api.post<BackupJob>('/backups/run');
      toast.info('Backup started.');
      track(job);
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      starting = false;
    }
  }
</script>

<PageHeader title="Backups" description="Encrypted, deduplicated snapshots of your sites and databases.">
  {#snippet meta()}
    {#if runningBackup}
      <Status kind="running" label={runningBackup.message || 'Backing up…'} />
    {:else if cfg.data?.configured && lastView}
      <Status kind={lastView.kind} label="Last backup: {lastView.label.toLowerCase()}" />
      {#if last?.last_run_at}<span class="muted small">{relative(last.last_run_at)}</span>{/if}
      {#if last?.enabled && last.next_run_at}<span class="muted small">· next {relative(last.next_run_at)}</span>{/if}
    {/if}
  {/snippet}
  {#snippet actions()}
    {#if cfg.data?.configured}
      <Button variant="primary" icon={Play} loading={starting || !!runningBackup} onclick={runNow}>
        {runningBackup ? 'Backing up…' : 'Back up now'}
      </Button>
    {/if}
  {/snippet}
</PageHeader>

<Tabs {tabs} {active} label="Backup sections" />

<div class="section">
  {#if cfg.loading}
    <Skeleton rows={4} />
  {:else if cfg.error || !cfg.data}
    <ErrorState message={cfg.error ?? 'Unknown error'} onretry={() => cfg.reload()} />
  {:else if active === 'snapshots'}
    <BackupSnapshots data={cfg.data} {revision} {jobs} {track} />
  {:else if active === 'destination'}
    <BackupDestination data={cfg.data} onsaved={(next) => cfg.set(next)} />
  {:else}
    <BackupActivity {revision} {jobs} />
  {/if}
</div>

<style>
  .section {
    margin-top: 24px;
  }
</style>
