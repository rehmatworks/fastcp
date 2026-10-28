<script lang="ts">
  import Dialog from '../components/Dialog.svelte';
  import Button from '../components/Button.svelte';
  import Input from '../components/Input.svelte';
  import { api, errorMessage } from '../lib/api';
  import { toast } from '../lib/toast.svelte';

  let { open, onclose }: { open: boolean; onclose: () => void } = $props();

  let current = $state('');
  let next = $state('');
  let confirm = $state('');
  let busy = $state(false);
  let error = $state<string | null>(null);

  $effect(() => {
    if (open) {
      current = next = confirm = '';
      error = null;
    }
  });

  const mismatch = $derived(confirm.length > 0 && next !== confirm);

  async function submit(e: Event) {
    e.preventDefault();
    if (next !== confirm) return;
    busy = true;
    error = null;
    try {
      await api.post('/auth/password', { current_password: current, new_password: next });
      toast.success('Your password has been changed.');
      onclose();
    } catch (err) {
      error = errorMessage(err);
    } finally {
      busy = false;
    }
  }
</script>

<Dialog {open} title="Change password" description="This changes the password for the panel, SFTP and SSH." {busy} {onclose}>
  <form id="pw-form" class="stack" onsubmit={submit}>
    <Input label="Current password" type="password" bind:value={current} autocomplete="current-password" required />
    <Input
      label="New password"
      type="password"
      bind:value={next}
      autocomplete="new-password"
      minlength={8}
      required
      hint="At least 8 characters."
    />
    <Input
      label="Confirm new password"
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
    <Button type="submit" form="pw-form" variant="primary" loading={busy} disabled={!current || next.length < 8 || next !== confirm}>
      Change password
    </Button>
  {/snippet}
</Dialog>
