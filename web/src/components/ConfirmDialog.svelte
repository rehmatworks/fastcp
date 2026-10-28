<!--
  Confirmation for destructive actions. When `confirmText` is set the user
  must type it exactly before the action is enabled.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';
  import Dialog from './Dialog.svelte';
  import Button from './Button.svelte';
  import Input from './Input.svelte';
  import { errorMessage } from '../lib/api';

  interface Props {
    open: boolean;
    title: string;
    message?: string;
    details?: Snippet;
    confirmText?: string;
    actionLabel?: string;
    destructive?: boolean;
    onconfirm: () => Promise<void> | void;
    onclose: () => void;
  }

  let {
    open,
    title,
    message,
    details,
    confirmText,
    actionLabel = 'Confirm',
    destructive = true,
    onconfirm,
    onclose,
  }: Props = $props();

  let typed = $state('');
  let busy = $state(false);
  let error = $state<string | null>(null);

  $effect(() => {
    if (open) {
      typed = '';
      error = null;
    }
  });

  const ready = $derived(!confirmText || typed.trim() === confirmText);

  async function submit(e: Event) {
    e.preventDefault();
    if (!ready || busy) return;
    busy = true;
    error = null;
    try {
      await onconfirm();
      onclose();
    } catch (err) {
      error = errorMessage(err);
    } finally {
      busy = false;
    }
  }
</script>

<Dialog {open} {title} size="sm" {busy} {onclose}>
  <form id="confirm-form" class="stack" onsubmit={submit}>
    {#if message}<p class="muted">{message}</p>{/if}
    {@render details?.()}
    {#if confirmText}
      <Input
        label="Type {confirmText} to confirm"
        bind:value={typed}
        mono
        autocomplete="off"
        autocapitalize="off"
        spellcheck={false}
      />
    {/if}
    {#if error}<p class="notice strong" role="alert">{error}</p>{/if}
  </form>
  {#snippet footer()}
    <Button onclick={onclose} disabled={busy}>Cancel</Button>
    <Button type="submit" form="confirm-form" variant={destructive ? 'danger' : 'primary'} loading={busy} disabled={!ready}>
      {actionLabel}
    </Button>
  {/snippet}
</Dialog>
