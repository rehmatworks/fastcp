<script lang="ts">
  import Plus from '@lucide/svelte/icons/plus';
  import KeyRound from '@lucide/svelte/icons/key-round';
  import Trash2 from '@lucide/svelte/icons/trash-2';
  import PageHeader from '../components/PageHeader.svelte';
  import Card from '../components/Card.svelte';
  import Button from '../components/Button.svelte';
  import CopyField from '../components/CopyField.svelte';
  import Skeleton from '../components/Skeleton.svelte';
  import ErrorState from '../components/ErrorState.svelte';
  import EmptyState from '../components/EmptyState.svelte';
  import Dialog from '../components/Dialog.svelte';
  import Input from '../components/Input.svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import { api, errorMessage, seg } from '../lib/api';
  import { Resource } from '../lib/page.svelte';
  import { session } from '../lib/session.svelte';
  import { toast } from '../lib/toast.svelte';
  import { date } from '../lib/format';
  import type { SSHKey } from '../lib/types';

  const keys = new Resource((signal) => api.get<SSHKey[]>('/ssh-keys', { signal }));

  const user = $derived(session.user?.username ?? '');
  const host = $derived(location.hostname || session.meta?.hostname || '');
  const port = $derived(session.meta?.ssh_port ?? 22);

  let adding = $state(false);
  let keyName = $state('');
  let keyText = $state('');
  let addBusy = $state(false);
  let addError = $state<string | null>(null);
  let removing = $state<SSHKey | null>(null);

  const looksLikeKey = $derived(/^(ssh-(ed25519|rsa|dss)|ecdsa-sha2-nistp\d+|sk-(ssh-ed25519|ecdsa-sha2-nistp256)@openssh\.com) [A-Za-z0-9+/=]+/.test(keyText.trim()));
  const keyErr = $derived(keyText.trim() && !looksLikeKey ? 'Paste a public key that starts with ssh-ed25519, ssh-rsa or ecdsa-sha2-…' : null);

  function openAdd() {
    keyName = '';
    keyText = '';
    addError = null;
    adding = true;
  }

  async function add(e: Event) {
    e.preventDefault();
    addBusy = true;
    addError = null;
    let name = keyName.trim();
    if (!name) {
      const comment = keyText.trim().split(/\s+/)[2];
      name = comment || 'SSH key';
    }
    try {
      await api.post<SSHKey>('/ssh-keys', { name, public_key: keyText.trim() });
      adding = false;
      toast.success(`${name} was added.`);
      await keys.reload({ quiet: true });
    } catch (err) {
      addError = errorMessage(err);
    } finally {
      addBusy = false;
    }
  }
</script>

<PageHeader title="SSH & SFTP" description="Connect to this account to upload files or run commands." />

<div class="stack">
  <Card title="Connection details" description="Sign in with your FastCP password, or with an SSH key added below.">
    <div class="grid-3">
      <CopyField label="Host" value={host} />
      <CopyField label="Port" value={String(port)} />
      <CopyField label="Username" value={user} />
    </div>
    <div class="grid-2 commands">
      <CopyField label="SSH" value="ssh -p {port} {user}@{host}" />
      <CopyField label="SFTP" value="sftp -P {port} {user}@{host}" />
    </div>
  </Card>

  <Card title="SSH keys" description="Keys added here can sign in to this account over SSH and SFTP." flush>
    {#snippet actions()}
      <Button size="sm" icon={Plus} onclick={openAdd}>Add key</Button>
    {/snippet}
    {#if keys.loading}
      <div class="pad"><Skeleton variant="lines" rows={2} /></div>
    {:else if keys.error}
      <div class="pad"><ErrorState message={keys.error} onretry={() => keys.reload()} /></div>
    {:else if (keys.data?.length ?? 0) === 0}
      <div class="pad">
        <EmptyState compact icon={KeyRound} title="No SSH keys" description="Add your public key to sign in without a password." />
      </div>
    {:else}
      <table class="table">
        <thead>
          <tr>
            <th>Name</th>
            <th>Fingerprint</th>
            <th>Added</th>
            <th class="actions"><span class="sr-only">Actions</span></th>
          </tr>
        </thead>
        <tbody>
          {#each keys.data ?? [] as k (k.id)}
            <tr>
              <td class="primary-cell cell-title">{k.name}</td>
              <td data-label="Fingerprint" class="mono fp">{k.fingerprint}</td>
              <td data-label="Added" class="muted nowrap">{date(k.created_at)}</td>
              <td class="actions">
                <Button size="sm" variant="ghost" icon={Trash2} iconOnly aria-label="Remove {k.name}" title="Remove" onclick={() => (removing = k)} />
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </Card>
</div>

<Dialog open={adding} title="Add SSH key" size="lg" busy={addBusy} onclose={() => (adding = false)}>
  <form id="add-key" class="stack" onsubmit={add}>
    <Input
      label="Public key"
      bind:value={keyText}
      multiline
      rows={5}
      mono
      required
      placeholder="ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA… you@laptop"
      error={keyErr ?? addError}
      hint="The contents of a .pub file, for example ~/.ssh/id_ed25519.pub. Never paste a private key."
    />
    <Input label="Name" bind:value={keyName} placeholder="Defaults to the key comment" maxlength={100} />
  </form>
  {#snippet footer()}
    <Button onclick={() => (adding = false)} disabled={addBusy}>Cancel</Button>
    <Button type="submit" form="add-key" variant="primary" loading={addBusy} disabled={!looksLikeKey}>Add key</Button>
  {/snippet}
</Dialog>

<ConfirmDialog
  open={removing !== null}
  title="Remove {removing?.name}?"
  message="Anyone using this key can no longer sign in with it."
  actionLabel="Remove key"
  onclose={() => (removing = null)}
  onconfirm={async () => {
    if (!removing) return;
    const n = removing.name;
    await api.del(`/ssh-keys/${seg(removing.id)}`);
    toast.success(`${n} was removed.`);
    await keys.reload({ quiet: true });
  }}
/>

<style>
  .commands {
    margin-top: 16px;
  }
  .pad {
    padding: 4px 18px 18px;
  }
  .fp {
    overflow-wrap: anywhere;
  }
  .table th:first-child,
  .table td:first-child {
    padding-left: 18px;
  }
</style>
