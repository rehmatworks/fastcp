<script lang="ts">
  import Server from '@lucide/svelte/icons/server';
  import Cloud from '@lucide/svelte/icons/cloud';
  import HardDrive from '@lucide/svelte/icons/hard-drive';
  import Cable from '@lucide/svelte/icons/cable';
  import PlugZap from '@lucide/svelte/icons/plug-zap';
  import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
  import CircleCheck from '@lucide/svelte/icons/circle-check';
  import CircleX from '@lucide/svelte/icons/circle-x';
  import Card from '../../components/Card.svelte';
  import Input from '../../components/Input.svelte';
  import Select from '../../components/Select.svelte';
  import Switch from '../../components/Switch.svelte';
  import Checkbox from '../../components/Checkbox.svelte';
  import Segmented from '../../components/Segmented.svelte';
  import Button from '../../components/Button.svelte';
  import CopyField from '../../components/CopyField.svelte';
  import SaveBar from '../../components/SaveBar.svelte';
  import { api, errorMessage, isAbort } from '../../lib/api';
  import { Form, lifetime } from '../../lib/page.svelte';
  import { pollJob } from '../../lib/jobs';
  import { session } from '../../lib/session.svelte';
  import { toast } from '../../lib/toast.svelte';
  import type { AgentJob, BackendType, BackupConfig, BackupConfigRequest, BackupsResponse, CronDescription } from '../../lib/types';

  let { data, onsaved }: { data: BackupsResponse; onsaved: (next: BackupsResponse) => void } = $props();
  const signal = lifetime();

  function fromConfig(c: BackupConfig, configured: boolean): BackupConfigRequest {
    return {
      backend_type: c.backend_type,
      local_path: c.local_path,
      rclone_repository: c.backend_type === 'rclone' ? c.repository : '',
      sftp_username: c.sftp_username,
      sftp_host: c.sftp_host,
      sftp_port: c.sftp_port || 22,
      sftp_path: c.sftp_path,
      s3_endpoint: c.s3_endpoint,
      s3_bucket: c.s3_bucket,
      s3_prefix: c.s3_prefix,
      s3_region: c.s3_region,
      s3_bucket_lookup: c.s3_bucket_lookup || 'auto',
      s3_list_objects_v1: c.s3_list_objects_v1,
      s3_access_key: '',
      s3_secret_key: '',
      s3_session_token: '',
      repository_password: '',
      enabled: configured ? c.enabled : true,
      schedule_cron: c.schedule_cron,
      exclude_site_ids: [...c.exclude_site_ids],
      exclude_database_ids: [...c.exclude_database_ids],
      keep_last: c.keep_last,
      keep_daily: c.keep_daily,
      keep_weekly: c.keep_weekly,
      keep_monthly: c.keep_monthly,
    };
  }

  // svelte-ignore state_referenced_locally
  const form = new Form(fromConfig(data.config, data.configured));
  const v = $derived(form.values);
  const saved = $derived(data.config);

  let error = $state<string | null>(null);
  let testing = $state(false);
  let testResult = $state<{ ok: boolean; message: string } | null>(null);
  let sftpKey = $state<string | null>(null);
  let keyBusy = $state(false);
  let rcloneJustInstalled = $state(false);
  const rcloneInstalled = $derived(rcloneJustInstalled || (data.rclone_installed ?? false));
  let installingRclone = $state(false);
  let schedule = $state<CronDescription | null>(null);

  const backends: { value: BackendType; label: string; icon: typeof Server }[] = [
    { value: 'sftp', label: 'SFTP', icon: Server },
    { value: 's3', label: 'S3', icon: Cloud },
    { value: 'local', label: 'Local folder', icon: HardDrive },
    { value: 'rclone', label: 'rclone', icon: Cable },
  ];

  const schedules = [
    { value: '0 2 * * *', label: 'Every day at 02:00' },
    { value: '0 */12 * * *', label: 'Every 12 hours' },
    { value: '0 */6 * * *', label: 'Every 6 hours' },
    { value: '0 * * * *', label: 'Every hour' },
    { value: '0 3 * * 0', label: 'Every Sunday at 03:00' },
    { value: '__custom', label: 'Custom schedule' },
  ];
  // svelte-ignore state_referenced_locally
  let schedulePreset = $state(schedules.some((s) => s.value === form.values.schedule_cron) ? form.values.schedule_cron : '__custom');

  $effect(() => {
    const expr = v.schedule_cron.trim();
    if (!expr) {
      schedule = null;
      return;
    }
    const c = new AbortController();
    const t = setTimeout(async () => {
      try {
        schedule = await api.post<CronDescription>('/cron/describe', { expression: expr }, { signal: c.signal });
      } catch (err) {
        if (!isAbort(err)) schedule = null;
      }
    }, 250);
    return () => {
      clearTimeout(t);
      c.abort();
    };
  });

  const backendChanged = $derived(data.configured && v.backend_type !== saved.backend_type);
  const needsPassword = $derived(!saved.has_password);
  const needsKeys = $derived(v.backend_type === 's3' && (!saved.has_s3_keys || saved.backend_type !== 's3'));
  const retentionErr = $derived(
    v.keep_last + v.keep_daily + v.keep_weekly + v.keep_monthly <= 0 ? 'Keep at least one snapshot.' : null,
  );
  const passwordErr = $derived(
    v.repository_password && (v.repository_password.length < 8 || v.repository_password.length > 256)
      ? 'Use 8 to 256 characters.'
      : null,
  );
  const incomplete = $derived.by(() => {
    if (needsPassword && !v.repository_password) return true;
    switch (v.backend_type) {
      case 'sftp':
        return !v.sftp_host || !v.sftp_username || !v.sftp_path;
      case 's3':
        return !v.s3_endpoint || !v.s3_bucket || (needsKeys && (!v.s3_access_key || !v.s3_secret_key));
      case 'local':
        return !v.local_path;
      case 'rclone':
        return !v.rclone_repository;
    }
    return false;
  });

  function toggle(list: 'exclude_site_ids' | 'exclude_database_ids', id: string, included: boolean) {
    const set = new Set(form.values[list]);
    if (included) set.delete(id);
    else set.add(id);
    form.values[list] = [...set];
  }

  function body(): BackupConfigRequest {
    return {
      ...form.values,
      sftp_port: Number(form.values.sftp_port) || 22,
      keep_last: Number(form.values.keep_last) || 0,
      keep_daily: Number(form.values.keep_daily) || 0,
      keep_weekly: Number(form.values.keep_weekly) || 0,
      keep_monthly: Number(form.values.keep_monthly) || 0,
    };
  }

  async function save(e: Event) {
    e.preventDefault();
    form.saving = true;
    error = null;
    try {
      const next = await api.put<BackupsResponse>('/backups/config', body());
      form.commit(fromConfig(next.config, true));
      onsaved(next);
      toast.success('Backup settings saved.');
    } catch (err) {
      error = errorMessage(err);
    } finally {
      form.saving = false;
    }
  }

  async function test() {
    testing = true;
    testResult = null;
    try {
      testResult = await api.post<{ ok: boolean; message: string }>('/backups/config/test', body(), { signal });
    } catch (err) {
      if (!isAbort(err)) testResult = { ok: false, message: errorMessage(err) };
    } finally {
      testing = false;
    }
  }

  async function showKey() {
    keyBusy = true;
    try {
      sftpKey = (await api.post<{ public_key: string }>('/backups/sftp-key')).public_key;
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      keyBusy = false;
    }
  }

  async function installRclone() {
    installingRclone = true;
    try {
      const job = await api.post<AgentJob>('/settings/rclone/install');
      const done = await pollJob((sig) => api.get<AgentJob>(`/jobs/${job.id}`, { signal: sig }), { signal });
      if (done.status === 'succeeded') {
        rcloneJustInstalled = true;
        toast.success('rclone is installed.');
      } else {
        toast.error(done.message || 'rclone could not be installed.');
      }
    } catch (err) {
      if (!isAbort(err)) toast.error(errorMessage(err));
    } finally {
      installingRclone = false;
    }
  }
</script>

<form id="backup-config" class="stack" onsubmit={save}>
  <Card title="Destination" description="Snapshots are encrypted on this server before they are uploaded.">
    <div class="stack">
      <Segmented label="Destination type" bind:value={form.values.backend_type} options={backends} />
      {#if backendChanged}
        <p class="notice strong">
          <TriangleAlert size={16} strokeWidth={1.75} aria-hidden="true" />
          <span
            >Switching destinations starts a new repository, encrypted with the saved password unless you enter a new one. Existing snapshots
            stay at the old destination and no longer appear here.</span
          >
        </p>
      {/if}

      {#if v.backend_type === 'sftp'}
        <div class="grid-2">
          <Input label="Host" bind:value={form.values.sftp_host} placeholder="backup.example.com" autocomplete="off" spellcheck={false} />
          <Input label="Port" type="number" bind:value={form.values.sftp_port} min={1} max={65535} />
          <Input label="Username" bind:value={form.values.sftp_username} autocomplete="off" spellcheck={false} />
          <Input label="Folder" bind:value={form.values.sftp_path} placeholder="/backups/{session.user?.username}" mono />
        </div>
        <div class="key-box stack-sm">
          <p class="small">
            FastCP signs in with a key that belongs to your account. Add the public key to <span class="mono">~/.ssh/authorized_keys</span> on the
            backup server.
          </p>
          {#if sftpKey}
            <CopyField value={sftpKey} multiline label="Backup public key" />
          {:else}
            <div><Button size="sm" loading={keyBusy} onclick={showKey}>Show public key</Button></div>
          {/if}
        </div>
      {:else if v.backend_type === 's3'}
        <div class="grid-2">
          <Input label="Endpoint" bind:value={form.values.s3_endpoint} placeholder="https://s3.amazonaws.com" mono />
          <Input label="Bucket" bind:value={form.values.s3_bucket} mono />
          <Input label="Prefix" bind:value={form.values.s3_prefix} placeholder="Optional, e.g. fastcp/{session.user?.username}" mono />
          <Input label="Region" bind:value={form.values.s3_region} placeholder="Optional, e.g. us-east-1" mono />
          <Input
            label="Access key ID"
            bind:value={form.values.s3_access_key}
            autocomplete="off"
            mono
            placeholder={needsKeys ? '' : 'Saved — leave empty to keep'}
          />
          <Input
            label="Secret access key"
            type="password"
            bind:value={form.values.s3_secret_key}
            autocomplete="new-password"
            placeholder={needsKeys ? '' : 'Saved — leave empty to keep'}
          />
          <Input label="Session token" type="password" bind:value={form.values.s3_session_token} autocomplete="off" hint="Only for temporary credentials." />
          <Select
            label="Bucket addressing"
            bind:value={form.values.s3_bucket_lookup}
            options={[
              { value: 'auto', label: 'Automatic' },
              { value: 'dns', label: 'Virtual-hosted (bucket.host)' },
              { value: 'path', label: 'Path style (host/bucket)' },
            ]}
          />
        </div>
        <Checkbox
          label="Use ListObjects v1"
          description="Only for older S3-compatible services that do not support v2."
          bind:checked={form.values.s3_list_objects_v1}
        />
      {:else if v.backend_type === 'local'}
        <Input
          label="Folder on this server"
          bind:value={form.values.local_path}
          placeholder="/mnt/backups/{session.user?.username}"
          mono
          hint="Your account must be able to write to it. A local folder does not protect against losing the server."
        />
      {:else}
        {#if !rcloneInstalled}
          <div class="notice strong">
            <TriangleAlert size={16} strokeWidth={1.75} aria-hidden="true" />
            <span class="grow">rclone is not installed on this server.</span>
            {#if session.isAdmin}
              <Button size="sm" loading={installingRclone} onclick={installRclone}>Install rclone</Button>
            {:else}
              <span class="small">Ask your administrator to install it.</span>
            {/if}
          </div>
        {/if}
        <Input
          label="Repository"
          bind:value={form.values.rclone_repository}
          placeholder="rclone:remote:fastcp"
          mono
          hint="Create the remote first by running rclone config over SSH as {session.user?.username}."
        />
      {/if}
    </div>
  </Card>

  <Card title="Encryption password" description="Every snapshot is encrypted with this password.">
    <div class="stack-sm">
      <Input
        label="Repository password"
        type="password"
        bind:value={form.values.repository_password}
        autocomplete="new-password"
        placeholder={needsPassword ? '' : 'Saved — leave empty to keep'}
        error={passwordErr}
        hint={needsPassword ? 'At least 8 characters. Use the existing password if the repository already exists.' : undefined}
      />
      <p class="muted small">Without this password the backups cannot be restored, even by an administrator. Keep a copy somewhere safe.</p>
    </div>
  </Card>

  <Card title="Schedule & retention">
    <div class="stack">
      <Switch label="Back up automatically" bind:checked={form.values.enabled} />
      <div class="grid-2">
        <Select
          label="Schedule"
          bind:value={schedulePreset}
          options={schedules}
          disabled={!v.enabled}
          onchange={(val) => {
            if (val !== '__custom') form.values.schedule_cron = val;
          }}
        />
        <Input
          label="Cron expression"
          bind:value={form.values.schedule_cron}
          mono
          disabled={!v.enabled}
          oninput={() => (schedulePreset = schedules.some((s) => s.value === form.values.schedule_cron.trim()) ? form.values.schedule_cron.trim() : '__custom')}
          error={schedule && !schedule.valid ? (schedule.message ?? 'Invalid schedule.') : null}
          hint={schedule?.valid ? `${schedule.description}. Runs must be at least an hour apart.` : 'Runs must be at least an hour apart.'}
        />
      </div>
      <div>
        <p class="label">Keep</p>
        <div class="grid-4">
          <Input label="Latest" type="number" bind:value={form.values.keep_last} min={0} max={1000} suffix="snapshots" />
          <Input label="Daily" type="number" bind:value={form.values.keep_daily} min={0} max={1000} suffix="days" />
          <Input label="Weekly" type="number" bind:value={form.values.keep_weekly} min={0} max={1000} suffix="weeks" />
          <Input label="Monthly" type="number" bind:value={form.values.keep_monthly} min={0} max={1000} suffix="months" />
        </div>
        <p class="muted small hint">{retentionErr ?? 'Older snapshots are removed after each backup. Space is reclaimed once a day.'}</p>
      </div>
    </div>
  </Card>

  <Card title="What to back up" description="New sites and databases are included automatically.">
    <div class="grid-2">
      <div class="stack-sm">
        <p class="label">Sites</p>
        {#each data.sites as s (s.id)}
          <Checkbox label={s.name} checked={!v.exclude_site_ids.includes(s.id)} onchange={(on) => toggle('exclude_site_ids', s.id, on)} />
        {:else}
          <p class="muted small">No sites.</p>
        {/each}
      </div>
      <div class="stack-sm">
        <p class="label">Databases</p>
        {#each data.databases as d (d.id)}
          <Checkbox label={d.name} checked={!v.exclude_database_ids.includes(d.id)} onchange={(on) => toggle('exclude_database_ids', d.id, on)} />
        {:else}
          <p class="muted small">No databases.</p>
        {/each}
      </div>
    </div>
  </Card>

  {#if testResult}
    <p class="notice" class:strong={!testResult.ok} role="status">
      {#if testResult.ok}<CircleCheck size={16} strokeWidth={1.75} aria-hidden="true" />{:else}<CircleX size={16} strokeWidth={2} aria-hidden="true" />{/if}
      <span class="result">{testResult.message}</span>
    </p>
  {/if}
  {#if error}<p class="notice strong" role="alert">{error}</p>{/if}

  <div class="actions-bar">
    <Button icon={PlugZap} loading={testing} disabled={incomplete || !!passwordErr} onclick={test}>Test connection</Button>
    {#if data.configured}
      <SaveBar
        form="backup-config"
        dirty={form.dirty}
        saving={form.saving}
        disabled={incomplete || !!retentionErr || !!passwordErr || schedule?.valid === false}
        onreset={() => form.reset()}
      />
    {:else}
      <span class="grow"></span>
      <Button
        type="submit"
        variant="primary"
        loading={form.saving}
        disabled={incomplete || !!retentionErr || !!passwordErr || schedule?.valid === false}
      >
        Save destination
      </Button>
    {/if}
  </div>
</form>

<style>
  .key-box {
    padding: 12px 14px;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--bg-subtle);
  }
  .label {
    font-weight: 500;
    font-size: 13px;
    margin-bottom: 8px;
  }
  .hint {
    margin-top: 8px;
  }
  .grow {
    flex: 1;
  }
  .notice {
    align-items: center;
  }
  .result {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .actions-bar {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 14px 0;
    border-top: 1px solid var(--border);
    position: sticky;
    bottom: 0;
    background: var(--bg);
  }
  .actions-bar :global(.save-bar) {
    flex: 1;
  }
</style>
