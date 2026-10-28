<script lang="ts">
  import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
  import Card from '../../components/Card.svelte';
  import Input from '../../components/Input.svelte';
  import Switch from '../../components/Switch.svelte';
  import SaveBar from '../../components/SaveBar.svelte';
  import Skeleton from '../../components/Skeleton.svelte';
  import ErrorState from '../../components/ErrorState.svelte';
  import FirewallCard from './FirewallCard.svelte';
  import { api, errorMessage } from '../../lib/api';
  import { Form, Resource } from '../../lib/page.svelte';
  import { session } from '../../lib/session.svelte';
  import { toast } from '../../lib/toast.svelte';
  import type { SSHConfig } from '../../lib/types';

  const ssh = new Resource((signal) => api.get<SSHConfig>('/settings/ssh', { signal }));
  const form = new Form<{ port: number; password_auth: boolean }>({ port: 22, password_auth: true });
  let seeded = false;
  $effect(() => {
    if (ssh.data && !seeded) {
      seeded = true;
      form.reset({ port: ssh.data.port, password_auth: ssh.data.password_auth });
    }
  });
  let error = $state<string | null>(null);
  let firewallRevision = $state(0);

  const portErr = $derived.by(() => {
    const n = Number(form.values.port);
    return Number.isInteger(n) && n >= 1 && n <= 65535 ? null : 'Between 1 and 65535.';
  });
  const portChanged = $derived(Number(form.values.port) !== form.saved.port);
  const lockout = $derived(!form.values.password_auth && ssh.data?.keys_installed === false);

  async function save(e: Event) {
    e.preventDefault();
    if (portErr || lockout) return;
    form.saving = true;
    error = null;
    try {
      const next = await api.put<SSHConfig>('/settings/ssh', { port: Number(form.values.port), password_auth: form.values.password_auth });
      ssh.set(next);
      form.commit({ port: next.port, password_auth: next.password_auth });
      void session.refreshMeta();
      firewallRevision++;
      toast.success(portChanged ? `SSH now listens on port ${next.port}.` : 'SSH settings saved.');
    } catch (err) {
      error = errorMessage(err);
    } finally {
      form.saving = false;
    }
  }
</script>

<div class="stack">
  {#if ssh.loading}
    <Skeleton variant="card" rows={1} />
  {:else if ssh.error || !ssh.data}
    <ErrorState message={ssh.error ?? 'Unknown error'} onretry={() => ssh.reload()} />
  {:else}
    <Card title="SSH" description="Applies to SSH and SFTP for every account.">
      <form id="ssh" class="stack" onsubmit={save}>
        <div class="grid-2">
          <Input label="Port" type="number" bind:value={form.values.port} min={1} max={65535} error={portErr} />
        </div>
        {#if portChanged && !portErr}
          <p class="notice strong">
            <TriangleAlert size={16} strokeWidth={1.75} aria-hidden="true" />
            <span>
              Keep an existing SSH session open until you have confirmed you can connect on port {form.values.port}. If ufw is active, the new
              port is opened automatically; open it in any cloud firewall yourself.
            </span>
          </p>
        {/if}
        <Switch
          label="Allow password logins"
          description="When off, only SSH keys are accepted. Panel logins are not affected."
          bind:checked={form.values.password_auth}
        />
        {#if lockout}
          <p class="notice strong" role="alert">
            <TriangleAlert size={16} strokeWidth={1.75} aria-hidden="true" />
            <span>No account has an SSH key yet. Add one under SSH &amp; SFTP first, or nobody could sign in.</span>
          </p>
        {/if}
        {#if error}<p class="notice strong" role="alert">{error}</p>{/if}
      </form>
      {#snippet footer()}
        <SaveBar form="ssh" dirty={form.dirty} saving={form.saving} disabled={!!portErr || lockout} onreset={() => form.reset()} />
      {/snippet}
    </Card>
  {/if}

  <FirewallCard revision={firewallRevision} />
</div>
