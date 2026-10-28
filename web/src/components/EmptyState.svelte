<script lang="ts">
  import type { Component, Snippet } from 'svelte';

  interface Props {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    icon?: Component<any>;
    title: string;
    description?: string;
    compact?: boolean;
    children?: Snippet;
  }
  let { icon: Icon, title, description, compact = false, children }: Props = $props();
</script>

<div class="empty" class:compact>
  {#if Icon}
    <div class="icon"><Icon size={20} strokeWidth={1.5} aria-hidden="true" /></div>
  {/if}
  <h3>{title}</h3>
  {#if description}<p class="muted">{description}</p>{/if}
  {#if children}<div class="actions">{@render children()}</div>{/if}
</div>

<style>
  .empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    text-align: center;
    gap: 6px;
    padding: 48px 24px;
    border: 1px dashed var(--border-strong);
    border-radius: var(--radius-lg);
  }
  .empty.compact {
    padding: 28px 20px;
  }
  .icon {
    display: grid;
    place-items: center;
    width: 40px;
    height: 40px;
    margin-bottom: 6px;
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    background: var(--bg-subtle);
  }
  p {
    max-width: 420px;
    font-size: 13.5px;
  }
  .actions {
    margin-top: 10px;
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
    justify-content: center;
  }
</style>
