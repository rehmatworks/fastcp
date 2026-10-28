<script lang="ts">
  import Globe from '@lucide/svelte/icons/globe';
  import DatabaseIcon from '@lucide/svelte/icons/database';
  import CalendarClock from '@lucide/svelte/icons/calendar-clock';
  import KeyRound from '@lucide/svelte/icons/key-round';
  import ArrowUpCircle from '@lucide/svelte/icons/circle-arrow-up';
  import Archive from '@lucide/svelte/icons/archive';
  import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
  import PageHeader from '../components/PageHeader.svelte';
  import Card from '../components/Card.svelte';
  import Stat from '../components/Stat.svelte';
  import Status from '../components/Status.svelte';
  import Skeleton from '../components/Skeleton.svelte';
  import ErrorState from '../components/ErrorState.svelte';
  import Button from '../components/Button.svelte';
  import EmptyState from '../components/EmptyState.svelte';
  import { api } from '../lib/api';
  import { Resource } from '../lib/page.svelte';
  import { session } from '../lib/session.svelte';
  import { bytes, duration, percent, relative, megabytes, limit, elapsed } from '../lib/format';
  import { backupJobView, phpSupportView, jobTypeLabels } from '../lib/status';
  import type { OverviewResponse } from '../lib/types';

  const overview = new Resource((signal) => api.get<OverviewResponse>('/overview', { signal }));

  $effect(() => {
    const t = setInterval(() => void overview.reload({ quiet: true }), 30_000);
    return () => clearInterval(t);
  });

  const acct = $derived(overview.data?.account);
  const server = $derived(overview.data?.server);
  const st = $derived(server?.status);
  const installedPHP = $derived(server?.php.filter((p) => p.installed) ?? []);
  const stoppedServices = $derived(server?.services.filter((s) => !s.active).length ?? 0);
  const backup = $derived(acct?.backup);
  const backupView = $derived(backup ? backupJobView(backup.last_status) : null);
</script>

<PageHeader
  title="Overview"
  description={session.isAdmin ? 'Server health and your account at a glance.' : 'Your sites, databases and backups at a glance.'}
/>

{#if overview.loading}
  <div class="stack">
    <Skeleton variant="card" rows={4} />
    <Skeleton rows={4} />
  </div>
{:else if overview.error}
  <ErrorState message={overview.error} onretry={() => overview.reload()} />
{:else if acct}
  <div class="stack-lg">
    {#if server}
      {#if server.update?.update_available}
        <div class="notice strong" role="status">
          <ArrowUpCircle size={16} strokeWidth={1.75} aria-hidden="true" />
          <p class="grow">FastCP {server.update.latest_version} is available. You are running {server.update.current_version}.</p>
          <Button size="sm" href="/settings/updates">Review update</Button>
        </div>
      {/if}
      {#if server.errors?.length}
        <div class="notice strong" role="alert">
          <TriangleAlert size={16} strokeWidth={1.75} aria-hidden="true" />
          <div>
            <p>Some server details could not be loaded:</p>
            <ul class="errors">
              {#each server.errors as e (e)}<li class="small">{e}</li>{/each}
            </ul>
          </div>
        </div>
      {/if}

      <section class="stack" aria-labelledby="server-h">
        <h2 id="server-h" class="section-title">Server</h2>
        {#if st}
          <div class="grid-4">
            <Stat
              label="Load"
              value={st.load_average[0].toFixed(2)}
              sub="{st.cpu_count} CPU{st.cpu_count === 1 ? '' : 's'} · 5m {st.load_average[1].toFixed(2)} · 15m {st.load_average[2].toFixed(2)}"
            />
            <Stat
              label="Memory"
              value="{percent(st.memory_used, st.memory_total)}%"
              meter={percent(st.memory_used, st.memory_total)}
              sub="{bytes(st.memory_used)} of {bytes(st.memory_total)}"
            />
            <Stat
              label="Disk"
              value="{percent(st.disk_used, st.disk_total)}%"
              meter={percent(st.disk_used, st.disk_total)}
              sub="{bytes(st.disk_used)} of {bytes(st.disk_total)}"
            />
            <Stat label="Uptime" value={duration(st.uptime)} sub={st.hostname} />
          </div>
        {/if}

        <div class="grid-2">
          <Card
            title="Services"
            description={server.services.length === 0 ? undefined : stoppedServices ? `${stoppedServices} not running` : 'All services are running.'}
            flush
          >
            <ul class="list">
              {#each server.services as svc (svc.name)}
                <li>
                  <div class="min">
                    <p class="cell-title">{svc.description || svc.name}</p>
                    <p class="muted small mono">{svc.name}</p>
                  </div>
                  {#if svc.active}
                    <Status kind="ok" label="Running" />
                  {:else}
                    <Status kind="error" label={svc.state ? svc.state[0].toUpperCase() + svc.state.slice(1) : 'Stopped'} />
                  {/if}
                </li>
              {:else}
                <li class="muted">Service status comes from systemd, which is not running on this server.</li>
              {/each}
            </ul>
          </Card>

          <div class="stack">
            <Card title="Platform">
              {#snippet actions()}
                <Button size="sm" variant="ghost" href="/settings/php">Manage PHP</Button>
              {/snippet}
              <dl class="kv">
                {#if st}
                  <dt>Operating system</dt>
                  <dd>{st.os || '—'}</dd>
                  <dt>Kernel</dt>
                  <dd class="mono">{st.kernel || '—'} · {st.architecture}</dd>
                  <dt>Web server</dt>
                  <dd>Caddy {st.caddy_version || '—'}</dd>
                  <dt>Database</dt>
                  <dd>{st.mysql_version || '—'}</dd>
                {/if}
                <dt>PHP</dt>
                <dd>
                  {#if installedPHP.length === 0}
                    <span class="muted">None installed</span>
                  {:else}
                    <ul class="php">
                      {#each installedPHP as p (p.version)}
                        {@const v = phpSupportView(p.status, p.eol_date)}
                        <li>
                          <span class="mono">{p.patch_version || p.version}</span>
                          {#if p.default}<span class="muted small">default</span>{/if}
                          {#if p.status !== 'active'}<Status kind={v.kind} label={v.label} title={v.title} />{/if}
                        </li>
                      {/each}
                    </ul>
                  {/if}
                </dd>
              </dl>
            </Card>
            <div class="grid-3">
              <Stat label="Users" value={server.users} href="/users" />
              <Stat label="Sites" value={server.sites} sub="All users" />
              <Stat label="Databases" value={server.databases} sub="All users" />
            </div>
          </div>
        </div>
      </section>
    {/if}

    <section class="stack" aria-labelledby="acct-h">
      <h2 id="acct-h" class="section-title">{server ? 'Your account' : session.me?.impersonating ? `${session.user?.username}'s account` : 'Your account'}</h2>
      <div class="grid-4">
        <Stat
          label="Sites"
          icon={Globe}
          value={acct.sites}
          sub={acct.limits.max_sites >= 0 ? `of ${acct.limits.max_sites} allowed` : undefined}
          href="/sites"
        />
        <Stat label="Databases" icon={DatabaseIcon} value={acct.databases} href="/databases" />
        <Stat label="Cron jobs" icon={CalendarClock} value={acct.cron_jobs} href="/cron" />
        <Stat label="SSH keys" icon={KeyRound} value={acct.ssh_keys} href="/access" />
      </div>

      <div class="grid-2">
        <Card title="Backups">
          {#snippet actions()}
            <Button size="sm" variant="ghost" href={backup?.configured ? '/backups' : '/backups/destination'}>
              {backup?.configured ? 'Open backups' : 'Set up'}
            </Button>
          {/snippet}
          {#if !backup?.configured}
            <EmptyState compact icon={Archive} title="Backups are not set up" description="Choose where to store snapshots of your sites and databases." />
          {:else}
            <dl class="kv">
              <dt>Schedule</dt>
              <dd>{backup.enabled ? 'On' : 'Paused'}</dd>
              <dt>Last backup</dt>
              <dd>
                {#if backupView}<Status kind={backupView.kind} label={backupView.label} />{/if}
                {#if backup.last_run_at}<span class="muted small"> · {relative(backup.last_run_at)}</span>{/if}
              </dd>
              {#if backup.enabled && backup.next_run_at}
                <dt>Next backup</dt>
                <dd>{relative(backup.next_run_at)}</dd>
              {/if}
            </dl>
          {/if}
        </Card>

        <Card title="Recent backup activity" flush>
          {#if acct.recent_jobs.length === 0}
            <p class="muted pad">Nothing yet.</p>
          {:else}
            <ul class="list">
              {#each acct.recent_jobs as job (job.id)}
                {@const v = backupJobView(job.status)}
                <li>
                  <div class="min">
                    <p class="cell-title">{jobTypeLabels[job.job_type] ?? job.job_type}</p>
                    <p class="muted small truncate" title={job.message}>{relative(job.started_at)} · {elapsed(job.started_at, job.finished_at)}</p>
                  </div>
                  <Status kind={v.kind} label={v.label} title={job.message} />
                </li>
              {/each}
            </ul>
          {/if}
        </Card>
      </div>

      {#if !server}
        <Card title="Resource limits" description="Set by your administrator.">
          <dl class="kv">
            <dt>Memory</dt>
            <dd>{megabytes(acct.limits.memory_mb)}</dd>
            <dt>CPU</dt>
            <dd>{acct.limits.cpu_percent < 0 ? 'Unlimited' : `${acct.limits.cpu_percent}% of one core`}</dd>
            <dt>Sites</dt>
            <dd>{limit(acct.limits.max_sites)}</dd>
            {#if acct.limits.storage_mb >= 0}
              <dt>Storage allowance</dt>
              <dd>{megabytes(acct.limits.storage_mb)}</dd>
            {/if}
          </dl>
        </Card>
      {/if}
    </section>
  </div>
{/if}

<style>
  .stack-lg {
    display: flex;
    flex-direction: column;
    gap: 32px;
  }
  .section-title {
    font-size: 13px;
    font-weight: 600;
    color: var(--fg-subtle);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .grow {
    flex: 1;
  }
  .notice {
    align-items: center;
  }
  .errors {
    margin: 4px 0 0;
    padding-left: 18px;
  }
  .list {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .list li {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 10px 18px;
    border-top: 1px solid var(--border);
  }
  .min {
    min-width: 0;
  }
  .cell-title {
    font-weight: 500;
  }
  .pad {
    padding: 4px 18px 18px;
  }
  .php {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .php li {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }
  .php li :global(.status) {
    white-space: normal;
  }
</style>
