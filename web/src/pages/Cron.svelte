<script lang="ts">
  import Plus from '@lucide/svelte/icons/plus';
  import CalendarClock from '@lucide/svelte/icons/calendar-clock';
  import Pencil from '@lucide/svelte/icons/pencil';
  import Trash2 from '@lucide/svelte/icons/trash-2';
  import PageHeader from '../components/PageHeader.svelte';
  import Button from '../components/Button.svelte';
  import Skeleton from '../components/Skeleton.svelte';
  import ErrorState from '../components/ErrorState.svelte';
  import EmptyState from '../components/EmptyState.svelte';
  import Switch from '../components/Switch.svelte';
  import Menu from '../components/Menu.svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import CronDrawer from './cron/CronDrawer.svelte';
  import { api, errorMessage, seg } from '../lib/api';
  import { Resource } from '../lib/page.svelte';
  import { toast } from '../lib/toast.svelte';
  import { relative, dateTime } from '../lib/format';
  import type { CronJob } from '../lib/types';

  const jobs = new Resource((signal) => api.get<CronJob[]>('/cron', { signal }));

  let editing = $state<CronJob | null>(null);
  let drawerOpen = $state(false);
  let deleting = $state<CronJob | null>(null);
  let toggling = $state<string | null>(null);

  function edit(job: CronJob | null) {
    editing = job;
    drawerOpen = true;
  }

  async function toggle(job: CronJob, on: boolean) {
    toggling = job.id;
    try {
      await api.put<CronJob>(`/cron/${seg(job.id)}`, { name: job.name, expression: job.expression, command: job.command, enabled: on });
      toast.success(on ? `${job.name} is enabled.` : `${job.name} is paused.`);
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      toggling = null;
      await jobs.reload({ quiet: true });
    }
  }
</script>

<PageHeader title="Cron jobs" description="Commands that run on a schedule, such as framework schedulers or cleanup scripts.">
  {#snippet actions()}
    <Button variant="primary" icon={Plus} onclick={() => edit(null)}>New cron job</Button>
  {/snippet}
</PageHeader>

{#if jobs.loading}
  <Skeleton rows={3} />
{:else if jobs.error}
  <ErrorState message={jobs.error} onretry={() => jobs.reload()} />
{:else if (jobs.data?.length ?? 0) === 0}
  <EmptyState icon={CalendarClock} title="No cron jobs yet" description="Schedule a command to run every few minutes, hourly, daily or on a custom schedule.">
    <Button variant="primary" icon={Plus} onclick={() => edit(null)}>New cron job</Button>
  </EmptyState>
{:else}
  <div class="table-wrap">
    <table class="table">
      <thead>
        <tr>
          <th>Job</th>
          <th>Schedule</th>
          <th>Next run</th>
          <th>Enabled</th>
          <th class="actions"><span class="sr-only">Actions</span></th>
        </tr>
      </thead>
      <tbody>
        {#each jobs.data ?? [] as job (job.id)}
          <tr>
            <td class="primary-cell">
              <div class="cell-title">{job.name}</div>
              <code class="cmd truncate" title={job.command}>{job.command}</code>
            </td>
            <td data-label="Schedule">
              <div>{job.description}</div>
              {#if job.description !== job.expression}<div class="cell-sub mono">{job.expression}</div>{/if}
            </td>
            <td data-label="Next run" class="nowrap">
              {#if job.enabled && job.next_run}
                <span title={dateTime(job.next_run)}>{relative(job.next_run)}</span>
              {:else}
                <span class="muted">Paused</span>
              {/if}
            </td>
            <td data-label="Enabled">
              <Switch label="Enable {job.name}" hideLabel checked={job.enabled} disabled={toggling === job.id} onchange={(on) => toggle(job, on)} />
            </td>
            <td class="actions">
              <Menu
                label="Actions for {job.name}"
                items={[
                  { label: 'Edit', icon: Pencil, onclick: () => edit(job) },
                  { label: 'Delete', icon: Trash2, destructive: true, onclick: () => (deleting = job) },
                ]}
              />
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}

<CronDrawer
  open={drawerOpen}
  job={editing}
  onclose={() => (drawerOpen = false)}
  onsaved={(job) => {
    drawerOpen = false;
    toast.success(editing ? `${job.name} was updated.` : `${job.name} was created.`);
    void jobs.reload({ quiet: true });
  }}
/>

<ConfirmDialog
  open={deleting !== null}
  title="Delete {deleting?.name}?"
  message="The job is removed from the schedule immediately."
  actionLabel="Delete cron job"
  onclose={() => (deleting = null)}
  onconfirm={async () => {
    if (!deleting) return;
    const n = deleting.name;
    await api.del(`/cron/${seg(deleting.id)}`);
    toast.success(`${n} was deleted.`);
    await jobs.reload({ quiet: true });
  }}
/>

<style>
  .cmd {
    display: block;
    max-width: 420px;
    margin-top: 2px;
    color: var(--fg-subtle);
  }
  @media (max-width: 720px) {
    .cmd {
      max-width: 100%;
    }
  }
</style>
