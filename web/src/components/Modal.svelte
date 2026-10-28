<!--
  Shared modal surface for Dialog and Drawer, built on the native <dialog>
  element: it traps focus, closes on Escape, and makes the page inert.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';
  import X from '@lucide/svelte/icons/x';

  interface Props {
    open: boolean;
    title: string;
    description?: string;
    kind: 'dialog' | 'drawer';
    size?: 'sm' | 'md' | 'lg';
    /** Prevent closing (for example while a request is running). */
    busy?: boolean;
    onclose: () => void;
    children: Snippet;
    footer?: Snippet;
  }

  let { open, title, description, kind, size = 'md', busy = false, onclose, children, footer }: Props = $props();
  let el: HTMLDialogElement | undefined = $state();
  let opener: Element | null = null;
  const uid = $props.id();

  $effect(() => {
    if (!el) return;
    if (open && !el.open) {
      opener = document.activeElement;
      el.showModal();
    } else if (!open && el.open) {
      el.close();
      if (opener instanceof HTMLElement && opener.isConnected) opener.focus();
    }
  });

  function requestClose() {
    if (!busy) onclose();
  }
</script>

<dialog
  bind:this={el}
  class="{kind} {size}"
  aria-labelledby="m-{uid}-title"
  aria-describedby={description ? `m-${uid}-desc` : undefined}
  oncancel={(e) => {
    e.preventDefault();
    requestClose();
  }}
  onclick={(e) => {
    if (e.target === el) requestClose();
  }}
>
  {#if open}
    <div class="surface">
      <header>
        <div class="titles">
          <h2 id="m-{uid}-title">{title}</h2>
          {#if description}<p id="m-{uid}-desc" class="muted">{description}</p>{/if}
        </div>
        <button type="button" class="close" aria-label="Close" onclick={requestClose} disabled={busy}>
          <X size={16} strokeWidth={1.75} aria-hidden="true" />
        </button>
      </header>
      <div class="body">{@render children()}</div>
      {#if footer}<footer>{@render footer()}</footer>{/if}
    </div>
  {/if}
</dialog>

<style>
  dialog {
    padding: 0;
    border: 1px solid var(--border);
    background: var(--bg);
    color: var(--fg);
    box-shadow: var(--shadow);
    max-height: 100dvh;
    overflow: visible;
  }
  dialog::backdrop {
    background: var(--overlay);
    animation: fade-in 0.15s var(--ease);
  }
  .surface {
    display: flex;
    flex-direction: column;
    max-height: inherit;
    height: 100%;
  }
  .dialog {
    width: min(480px, calc(100vw - 32px));
    max-height: calc(100dvh - 64px);
    border-radius: var(--radius-lg);
    margin: auto;
    animation: pop-in 0.16s var(--ease);
  }
  .dialog.sm {
    width: min(400px, calc(100vw - 32px));
  }
  .dialog.lg {
    width: min(640px, calc(100vw - 32px));
  }
  .drawer {
    width: min(520px, 100vw);
    height: 100dvh;
    max-height: 100dvh;
    margin: 0 0 0 auto;
    border-width: 0 0 0 1px;
    border-radius: 0;
    animation: slide-in 0.18s var(--ease);
  }
  .drawer.lg {
    width: min(640px, 100vw);
  }
  header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 16px;
    padding: 18px 20px 14px;
  }
  .drawer header {
    border-bottom: 1px solid var(--border);
  }
  .titles {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
  }
  .titles p {
    font-size: 13px;
  }
  .close {
    display: grid;
    place-items: center;
    width: 28px;
    height: 28px;
    margin: -4px -6px 0 0;
    border: 0;
    border-radius: var(--radius);
    background: transparent;
    color: var(--fg-subtle);
    cursor: pointer;
    flex-shrink: 0;
  }
  .close:hover:not(:disabled) {
    background: var(--bg-muted);
    color: var(--fg);
  }
  .body {
    padding: 4px 20px 20px;
    overflow-y: auto;
    flex: 1;
    min-height: 0;
  }
  .drawer .body {
    padding: 20px;
  }
  footer {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    padding: 14px 20px;
    border-top: 1px solid var(--border);
    background: var(--bg-subtle);
    flex-wrap: wrap;
  }
  .dialog footer {
    border-radius: 0 0 var(--radius-lg) var(--radius-lg);
  }
</style>
