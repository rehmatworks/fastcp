<script lang="ts">
  import CircleAlert from '@lucide/svelte/icons/circle-alert';
  import RotateCw from '@lucide/svelte/icons/rotate-cw';
  import Button from './Button.svelte';

  let { message, onretry }: { message: string; onretry?: () => void } = $props();
</script>

<div class="notice strong error" role="alert">
  <CircleAlert size={16} strokeWidth={1.75} aria-hidden="true" />
  <div class="text">
    <p><strong>Could not load this page.</strong></p>
    <p class="muted">{message}</p>
  </div>
  {#if onretry}<Button size="sm" icon={RotateCw} onclick={onretry}>Retry</Button>{/if}
</div>

<style>
  .error {
    align-items: center;
  }
  .text {
    flex: 1;
    min-width: 0;
  }
</style>
