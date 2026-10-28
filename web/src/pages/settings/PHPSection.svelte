<script lang="ts">
  import Download from '@lucide/svelte/icons/download';
  import RefreshCw from '@lucide/svelte/icons/refresh-cw';
  import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
  import Card from '../../components/Card.svelte';
  import Button from '../../components/Button.svelte';
  import Badge from '../../components/Badge.svelte';
  import Status from '../../components/Status.svelte';
  import Select from '../../components/Select.svelte';
  import Switch from '../../components/Switch.svelte';
  import Input from '../../components/Input.svelte';
  import SaveBar from '../../components/SaveBar.svelte';
  import Skeleton from '../../components/Skeleton.svelte';
  import ErrorState from '../../components/ErrorState.svelte';
  import { api, errorMessage, isAbort } from '../../lib/api';
  import { Form, Resource, lifetime } from '../../lib/page.svelte';
  import { pollJob } from '../../lib/jobs';
  import { session } from '../../lib/session.svelte';
  import { toast } from '../../lib/toast.svelte';
  import { dateTime, pad2, relative } from '../../lib/format';
  import { agentJobView, phpSupportView } from '../../lib/status';
  import type { AgentJob, PHPStatus } from '../../lib/types';

  const signal = lifetime();
  const status = new Resource((sig) => api.get<PHPStatus>('/settings/php', { signal: sig }));

  interface Policy {
    default_version: string;
    auto_patch: boolean;
    auto_install: boolean;
    follow_latest_default: boolean;
    time: string;
  }
  function toPolicy(st: PHPStatus): Policy {
    const s = st.settings;
    return {
      default_version: s.default_version || (st.versions.find((v) => v.default)?.version ?? ''),
      auto_patch: s.auto_patch,
      auto_install: s.auto_install,
      follow_latest_default: s.follow_latest_default,
      time: `${pad2(s.maintenance_hour)}:${pad2(s.maintenance_minute)}`,
    };
  }

  const form = new Form<Policy>({ default_version: '', auto_patch: true, auto_install: true, follow_latest_default: false, time: '03:30' });
  let seeded = false;
  $effect(() => {
    if (status.data && !seeded) {
      seeded = true;
      form.reset(toPolicy(status.data));
    }
  });

  let error = $state<string | null>(null);
  let installing = $state<Record<string, AgentJob>>({});
  let maintenance = $state<AgentJob | null>(null);

  const installed = $derived((status.data?.versions ?? []).filter((v) => v.installed));
  const timeErr = $derived(/^([01]\d|2[0-3]):[0-5]\d$/.test(form.values.time) ? null : 'Use a time such as 03:30.');
  const maintenanceBusy = $derived(
    maintenance !== null || status.data?.last_maintenance?.status === 'running' || status.data?.last_maintenance?.status === 'queued',
  );

  // Follow jobs that were already running when the page opened.
  let resumed = false;
  $effect(() => {
    const jobs = status.data?.jobs;
    if (!jobs || resumed) return;
    resumed = true;
    for (const j of jobs) {
      if (j.status !== 'running' && j.status !== 'queued') continue;
      if (j.kind === 'php.install' && j.target) void follow(j, j.target);
      if (j.kind === 'php.maintenance') void followMaintenance(j);
    }
  });

  async function follow(job: AgentJob, version: string) {
    installing[version] = job;
    try {
      const done = await pollJob((sig) => api.get<AgentJob>(`/jobs/${job.id}`, { signal: sig }), {
        signal,
        onUpdate: (j) => (installing[version] = j),
      });
      if (done.status === 'succeeded') toast.success(`PHP ${version} is installed.`);
      else toast.error(done.message || `PHP ${version} could not be installed.`);
    } catch (err) {
      if (!isAbort(err)) toast.error(errorMessage(err));
    } finally {
      delete installing[version];
      if (!signal.aborted) {
        void status.reload({ quiet: true });
        void session.refreshMeta();
      }
    }
  }

  async function install(version: string) {
    try {
      const job = await api.post<AgentJob>('/settings/php/install', { version });
      void follow(job, version);
    } catch (err) {
      toast.error(errorMessage(err));
    }
  }

  async function followMaintenance(job: AgentJob) {
    maintenance = job;
    try {
      const done = await pollJob((sig) => api.get<AgentJob>(`/jobs/${job.id}`, { signal: sig }), {
        signal,
        onUpdate: (j) => (maintenance = j),
      });
      if (done.status === 'succeeded') toast.success(done.message || 'PHP maintenance finished.');
      else toast.error(done.message || 'PHP maintenance failed.');
    } catch (err) {
      if (!isAbort(err)) toast.error(errorMessage(err));
    } finally {
      maintenance = null;
      if (!signal.aborted) {
        void status.reload({ quiet: true });
        void session.refreshMeta();
      }
    }
  }

  async function runMaintenance() {
    try {
      const job = await api.post<AgentJob>('/settings/php/maintenance');
      void followMaintenance(job);
    } catch (err) {
      toast.error(errorMessage(err));
    }
  }

  async function save(e: Event) {
    e.preventDefault();
    if (timeErr) return;
    form.saving = true;
    error = null;
    const [h, m] = form.values.time.split(':').map(Number);
    try {
      const next = await api.put<PHPStatus>('/settings/php', {
        default_version: form.values.default_version,
        auto_patch: form.values.auto_patch,
        auto_install: form.values.auto_install,
        follow_latest_default: form.values.follow_latest_default,
        maintenance_hour: h,
        maintenance_minute: m,
      });
      status.set(next);
      form.commit(toPolicy(next));
      void session.refreshMeta();
      toast.success('PHP settings saved.');
    } catch (err) {
      error = errorMessage(err);
    } finally {
      form.saving = false;
    }
  }
</script>

{#if status.loading}
  <Skeleton variant="card" rows={2} />
{:else if status.error || !status.data}
  <ErrorState message={status.error ?? 'Unknown error'} onretry={() => status.reload()} />
{:else}
  {@const st = status.data}
  <div class="stack">
    {#if st.repository_error}
      <p class="notice strong" role="alert">
        <TriangleAlert size={16} strokeWidth={1.75} aria-hidden="true" />
        <span>The PHP package repository ({st.repository}) could not be read: {st.repository_error}</span>
      </p>
    {/if}

    <Card title="PHP versions" description="Packages come from {st.repository || 'the system repositories'}." flush>
      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr>
              <th>Version</th>
              <th>Installed</th>
              <th>Support</th>
              <th>Sites</th>
              <th class="actions"><span class="sr-only">Actions</span></th>
            </tr>
          </thead>
          <tbody>
            {#each st.versions as v (v.version)}
              {@const support = phpSupportView(v.status, v.eol_date)}
              {@const job = installing[v.version]}
              <tr>
                <td class="primary-cell">
                  <div class="row version">
                    <span class="cell-title">PHP {v.version}</span>
                    {#if v.default}<Badge strong>Default</Badge>{/if}
                  </div>
                </td>
                <td data-label="Installed">
                  {#if v.installed}
                    <span class="mono">{v.patch_version || v.version}</span>
                    {#if v.candidate}<div class="cell-sub">{v.candidate} available</div>{/if}
                    {#if !v.fpm_running}<div><Status kind="error" label="PHP-FPM stopped" /></div>{/if}
                  {:else}
                    <span class="muted">Not installed</span>
                  {/if}
                </td>
                <td data-label="Support"><Status kind={support.kind} label={support.label} title={support.title} /></td>
                <td data-label="Sites">{v.installed ? v.sites : '—'}</td>
                <td class="actions">
                  {#if job}
                    <Status kind="running" label={job.status === 'queued' ? 'Queued' : 'Installing…'} />
                  {:else if !v.installed && v.available}
                    <Button size="sm" icon={Download} onclick={() => install(v.version)}>Install</Button>
                  {:else if !v.installed}
                    <span class="muted small">Unavailable</span>
                  {/if}
                </td>
              </tr>
            {:else}
              <tr><td colspan="5" class="muted">No PHP versions found.</td></tr>
            {/each}
          </tbody>
        </table>
      </div>
    </Card>

    <Card title="Updates & defaults" description="FastCP keeps PHP current with a daily maintenance run.">
      <form id="php-policy" class="stack" onsubmit={save}>
        <Switch
          label="Install patch releases automatically"
          description="Security and bug-fix releases within a version, for example 8.4.11 to 8.4.12. PHP-FPM is reloaded without downtime."
          bind:checked={form.values.auto_patch}
        />
        <Switch
          label="Install new PHP versions automatically"
          description="When a new version such as PHP 8.5 is released, it is installed and becomes available to every site."
          bind:checked={form.values.auto_install}
        />
        <Switch
          label="Make the newest version the default"
          description="New sites and phpMyAdmin use the newest installed version. Existing sites keep their version."
          bind:checked={form.values.follow_latest_default}
        />
        <div class="grid-2">
          <Select
            label="Default version"
            bind:value={form.values.default_version}
            disabled={form.values.follow_latest_default}
            options={installed.map((v) => ({ value: v.version, label: `PHP ${v.version}` }))}
            hint="Used for new sites and phpMyAdmin."
          />
          <Input label="Maintenance time" type="time" bind:value={form.values.time} error={timeErr} hint="Server time, once a day." />
        </div>
        {#if error}<p class="notice strong" role="alert">{error}</p>{/if}
      </form>
      {#snippet footer()}
        <SaveBar form="php-policy" dirty={form.dirty} saving={form.saving} disabled={!!timeErr} onreset={() => form.reset()} />
      {/snippet}
    </Card>

    <Card title="Maintenance">
      {#snippet actions()}
        <Button size="sm" icon={RefreshCw} loading={maintenanceBusy} onclick={runMaintenance}>Run now</Button>
      {/snippet}
      <div class="stack">
        <dl class="kv">
          <dt>Last run</dt>
          <dd>
            {#if maintenance}
              <Status kind="running" label={maintenance.message || 'Running…'} />
            {:else if st.last_maintenance}
              {@const view = agentJobView(st.last_maintenance.status)}
              <Status kind={view.kind} label={view.label} />
              <span class="muted small" title={dateTime(st.last_maintenance.finished_at ?? st.last_maintenance.started_at)}>
                {relative(st.last_maintenance.finished_at ?? st.last_maintenance.started_at)}
              </span>
              {#if st.last_maintenance.message}<div class="muted small">{st.last_maintenance.message}</div>{/if}
            {:else}
              <span class="muted">Never</span>
            {/if}
          </dd>
          <dt>Next run</dt>
          <dd>
            {#if st.next_maintenance}
              {dateTime(st.next_maintenance)} <span class="muted small">({relative(st.next_maintenance)})</span>
            {:else}
              <span class="muted">Automatic updates are off</span>
            {/if}
          </dd>
        </dl>
        {#if st.jobs.length}
          <ul class="jobs">
            {#each st.jobs as j (j.id)}
              {@const view = agentJobView(j.status)}
              <li>
                <Status kind={view.kind} label={j.kind === 'php.install' ? `Install PHP ${j.target}` : 'Maintenance'} />
                <span class="muted small grow truncate" title={j.message}>{j.message}</span>
                <span class="muted small nowrap">{relative(j.finished_at ?? j.started_at)}</span>
              </li>
            {/each}
          </ul>
        {/if}
      </div>
    </Card>
  </div>
{/if}

<style>
  .version {
    gap: 8px;
  }
  .jobs {
    list-style: none;
    margin: 0;
    padding: 14px 0 0;
    border-top: 1px solid var(--border);
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .jobs li {
    display: flex;
    align-items: center;
    gap: 12px;
    min-width: 0;
  }
  .grow {
    flex: 1;
    min-width: 0;
  }
</style>
