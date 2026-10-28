<script lang="ts">
  import Dialog from '../../components/Dialog.svelte';
  import Button from '../../components/Button.svelte';
  import Input from '../../components/Input.svelte';
  import { api, errorMessage, seg } from '../../lib/api';
  import { toast } from '../../lib/toast.svelte';
  import { passwordError } from '../../lib/validate';

  let { username, onclose }: { username: string | null; onclose: () => void } = $props();

  let password = $state('');
  let confirm = $state('');
  let busy = $state(false);
  let error = $state<string | null>(null);

  $effect(() => {
    if (username) {
      password = '';
      confirm = '';
      error = null;
    }
  });

  const pwErr = $derived(password ? passwordError(password) : null);
  const mismatch = $derived(confirm.length > 0 && confirm !== password);

  async function submit(e: Event) {
    e.preventDefault();
    if (!username || pwErr || mismatch) return;
    busy = true;
    error = null;
    try {
      await api.post(`/users/${seg(username)}/password`, { password });
      toast.success(`Password changed for ${username}.`);
      onclose();
    } catch (err) {
      error = errorMessage(err);
    } finally {
      busy = false;
    }
  }
</script>

<Dialog open={username !== null} title="Set password for {username}" {busy} {onclose}>
  <form id="set-password" class="stack" onsubmit={submit}>
    <p class="muted">The new password applies to the panel, SSH and SFTP. {username} is signed out everywhere.</p>
    <Input label="New password" type="password" bind:value={password} autocomplete="new-password" required error={pwErr} />
    <Input
      label="Confirm password"
      type="password"
      bind:value={confirm}
      autocomplete="new-password"
      required
      error={mismatch ? 'The passwords do not match.' : null}
    />
    {#if error}<p class="notice strong" role="alert">{error}</p>{/if}
  </form>
  {#snippet footer()}
    <Button onclick={onclose} disabled={busy}>Cancel</Button>
    <Button type="submit" form="set-password" variant="primary" loading={busy} disabled={!password || !!pwErr || confirm !== password}>
      Set password
    </Button>
  {/snippet}
</Dialog>
