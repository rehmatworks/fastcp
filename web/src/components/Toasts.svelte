<script lang="ts">
  import CircleCheck from '@lucide/svelte/icons/circle-check';
  import CircleAlert from '@lucide/svelte/icons/circle-alert';
  import Info from '@lucide/svelte/icons/info';
  import X from '@lucide/svelte/icons/x';
  import { toast } from '../lib/toast.svelte';

  let el: HTMLDivElement | undefined = $state();

  // Re-show the popover whenever toasts change so it stays above any open
  // modal dialog in the top layer.
  $effect(() => {
    const n = toast.items.length;
    if (!el || typeof el.showPopover !== 'function') return;
    try {
      if (el.matches(':popover-open')) el.hidePopover();
      if (n > 0) el.showPopover();
    } catch {
      // popover unsupported
    }
  });

  const icons = { success: CircleCheck, error: CircleAlert, info: Info };
</script>

<div class="toasts" bind:this={el} popover="manual" aria-live="polite" role="status">
  {#each toast.items as t (t.id)}
    {@const Icon = icons[t.kind]}
    <div class="toast {t.kind}" role={t.kind === 'error' ? 'alert' : undefined}>
      <Icon size={16} strokeWidth={1.75} aria-hidden="true" />
      <p>{t.message}</p>
      <button type="button" aria-label="Dismiss" onclick={() => toast.dismiss(t.id)}>
        <X size={14} strokeWidth={1.75} aria-hidden="true" />
      </button>
    </div>
  {/each}
</div>

<style>
  .toasts {
    position: fixed;
    inset: auto 16px 16px auto;
    margin: 0;
    padding: 0;
    border: 0;
    background: transparent;
    overflow: visible;
    display: flex;
    flex-direction: column;
    gap: 8px;
    width: min(380px, calc(100vw - 32px));
    z-index: 100;
    pointer-events: none;
  }
  .toasts:not(:popover-open) {
    display: flex;
  }
  .toast {
    pointer-events: auto;
    display: flex;
    gap: 10px;
    align-items: flex-start;
    padding: 12px 12px 12px 14px;
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-lg);
    background: var(--bg);
    color: var(--fg);
    box-shadow: var(--shadow);
    animation: pop-in 0.18s var(--ease);
  }
  .toast.error {
    border-color: var(--fg);
  }
  .toast :global(svg) {
    flex-shrink: 0;
    margin-top: 1px;
  }
  p {
    flex: 1;
    font-size: 13.5px;
    overflow-wrap: anywhere;
  }
  button {
    display: grid;
    place-items: center;
    width: 22px;
    height: 22px;
    border: 0;
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--fg-subtle);
    cursor: pointer;
  }
  button:hover {
    background: var(--bg-muted);
    color: var(--fg);
  }
</style>
