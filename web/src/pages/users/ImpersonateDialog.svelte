<script lang="ts">
  import Dialog from '../../components/Dialog.svelte';
  import Button from '../../components/Button.svelte';
  import Input from '../../components/Input.svelte';
  import { errorMessage } from '../../lib/api';
  import { session } from '../../lib/session.svelte';

  let { username, onclose }: { username: string | null; onclose: () => void } = $props();

  let password = $state('');
  let reason = $state('');
  let busy = $state(false);
  let error = $state<string | null>(null);
  let field = $state<HTMLInputElement | HTMLTextAreaElement | null>(null);

  $effect(() => {
    if (username) {
      password = '';
      reason = '';
      error = null;
      queueMicrotask(() => field?.focus());
    }
  });

  async function submit(e: Event) {
    e.preventDefault();
    if (!username) return;
    busy = true;
    error = null;
    try {
      await session.startImpersonation(username, password, reason.trim());
      onclose();
    } catch (err) {
      error = errorMessage(err);
    } finally {
      busy = false;
    }
  }
</script>

<Dialog open={username !== null} title="Sign in as {username}" {busy} {onclose}>
  <form id="impersonate" class="stack" onsubmit={submit}>
    <p class="muted">
      You see and change everything as {username} until you return to your account or two hours pass. The session is recorded in the audit log.
    </p>
    <Input label="Your password" type="password" bind:value={password} bind:input={field} autocomplete="current-password" required />
    <Input label="Reason" bind:value={reason} placeholder="Optional, e.g. support ticket #123" maxlength={500} />
    {#if error}<p class="notice strong" role="alert">{error}</p>{/if}
  </form>
  {#snippet footer()}
    <Button onclick={onclose} disabled={busy}>Cancel</Button>
    <Button type="submit" form="impersonate" variant="primary" loading={busy} disabled={!password}>Sign in as {username}</Button>
  {/snippet}
</Dialog>
