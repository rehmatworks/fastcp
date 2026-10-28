<script lang="ts">
  import Drawer from '../../components/Drawer.svelte';
  import Button from '../../components/Button.svelte';
  import Input from '../../components/Input.svelte';
  import Switch from '../../components/Switch.svelte';
  import LimitField from './LimitField.svelte';
  import { api, errorMessage, seg } from '../../lib/api';
  import { session } from '../../lib/session.svelte';
  import { toast } from '../../lib/toast.svelte';
  import { passwordError, usernameError } from '../../lib/validate';
  import type { User } from '../../lib/types';

  interface Props {
    open: boolean;
    /** The user being edited, or null to create one. */
    user: User | null;
    onclose: () => void;
    onsaved: (user: User) => void;
  }
  let { open, user, onclose, onsaved }: Props = $props();

  let username = $state('');
  let password = $state('');
  let isAdmin = $state(false);
  let memory = $state(512);
  let cpu = $state(100);
  let maxSites = $state(-1);
  let storage = $state(-1);
  let busy = $state(false);
  let error = $state<string | null>(null);
  let touched = $state(false);

  $effect(() => {
    if (!open) return;
    username = user?.username ?? '';
    password = '';
    isAdmin = user?.is_admin ?? false;
    memory = user?.memory_mb ?? 512;
    cpu = user?.cpu_percent ?? 100;
    maxSites = user?.max_sites ?? -1;
    storage = user?.storage_mb ?? -1;
    error = null;
    touched = false;
  });

  const editing = $derived(user !== null);
  const isSelf = $derived(user?.username === session.me?.actor.username);

  function rangeError(v: number, min: number, max: number, what: string): string | null {
    if (v === -1) return null;
    if (!Number.isInteger(v) || v < min || v > max) return `${what} must be between ${min} and ${max}.`;
    return null;
  }
  const errors = $derived({
    username: editing || !touched || !username ? null : usernameError(username),
    password: editing || !password ? null : passwordError(password),
    memory: rangeError(memory, 128, 262144, 'Memory'),
    cpu: rangeError(cpu, 10, 4000, 'CPU'),
    maxSites: rangeError(maxSites, 0, 100000, 'The site limit'),
    storage: rangeError(storage, 0, 100_000_000, 'Storage'),
  });
  const invalid = $derived(
    Object.values(errors).some(Boolean) || (!editing && (!username || !password || !!usernameError(username))),
  );

  async function submit(e: Event) {
    e.preventDefault();
    if (invalid) return;
    busy = true;
    error = null;
    const limits = { memory_mb: memory, cpu_percent: cpu, max_sites: maxSites, storage_mb: storage };
    try {
      const saved = editing
        ? await api.patch<User>(`/users/${seg(user!.username)}`, { is_admin: isAdmin, ...limits })
        : await api.post<User>('/users', { username: username.trim(), password, is_admin: isAdmin, ...limits });
      toast.success(editing ? `${saved.username} updated.` : `${saved.username} created.`);
      onsaved(saved);
      onclose();
    } catch (err) {
      error = errorMessage(err);
    } finally {
      busy = false;
    }
  }
</script>

<Drawer
  {open}
  title={editing ? `Edit ${user?.username}` : 'New user'}
  description={editing ? undefined : 'Creates a Linux account with its own home folder, PHP pools and resource limits.'}
  {busy}
  {onclose}
>
  <form id="user-form" class="stack" onsubmit={submit}>
    {#if !editing}
      <Input
        label="Username"
        bind:value={username}
        onblur={() => (touched = true)}
        autocomplete="off"
        autocapitalize="off"
        spellcheck={false}
        mono
        required
        error={errors.username}
        hint="Lowercase letters, digits, - and _. Also the SSH and SFTP login."
      />
      <Input
        label="Password"
        type="password"
        bind:value={password}
        autocomplete="new-password"
        required
        error={errors.password}
        hint="At least 8 characters. Used for the panel, SSH and SFTP."
      />
    {/if}

    <Switch
      label="Administrator"
      description={isSelf ? 'You cannot remove your own administrator access.' : 'Administrators manage users and server settings.'}
      bind:checked={isAdmin}
      disabled={isSelf && isAdmin}
    />

    <div class="divider"></div>
    <div>
      <p class="label">Resource limits</p>
      <p class="muted small">Applied to every process the account runs, including PHP, cron jobs and SSH sessions.</p>
    </div>
    <div class="grid-2">
      <LimitField label="Memory" unit="MB" bind:value={memory} min={128} max={262144} fallback={512} error={errors.memory} />
      <LimitField
        label="CPU"
        unit="%"
        bind:value={cpu}
        min={10}
        max={4000}
        fallback={100}
        hint="100% is one core."
        error={errors.cpu}
      />
      <LimitField label="Sites" unit="sites" bind:value={maxSites} min={0} fallback={5} error={errors.maxSites} />
      <LimitField
        label="Storage allowance"
        unit="MB"
        bind:value={storage}
        min={0}
        fallback={10240}
        hint="Shown to the user; not enforced on disk."
        error={errors.storage}
      />
    </div>
    {#if error}<p class="notice strong" role="alert">{error}</p>{/if}
  </form>
  {#snippet footer()}
    <Button onclick={onclose} disabled={busy}>Cancel</Button>
    <Button type="submit" form="user-form" variant="primary" loading={busy} disabled={invalid}>
      {editing ? 'Save changes' : 'Create user'}
    </Button>
  {/snippet}
</Drawer>

<style>
  .label {
    font-weight: 500;
    font-size: 13px;
    margin-bottom: 2px;
  }
</style>
