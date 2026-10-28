<script lang="ts">
  import type { Snippet } from 'svelte';
  import ChevronLeft from '@lucide/svelte/icons/chevron-left';

  interface Props {
    title: string;
    description?: string;
    back?: { href: string; label: string };
    actions?: Snippet;
    meta?: Snippet;
  }
  let { title, description, back, actions, meta }: Props = $props();
</script>

<header class="page-header">
  {#if back}
    <a class="back" href={back.href}><ChevronLeft size={14} strokeWidth={1.75} aria-hidden="true" />{back.label}</a>
  {/if}
  <div class="row-main">
    <div class="titles">
      <h1>{title}</h1>
      {#if description}<p class="muted">{description}</p>{/if}
      {#if meta}<div class="meta">{@render meta()}</div>{/if}
    </div>
    {#if actions}<div class="actions">{@render actions()}</div>{/if}
  </div>
</header>

<style>
  .page-header {
    display: flex;
    flex-direction: column;
    gap: 10px;
    margin-bottom: 24px;
  }
  .back {
    display: inline-flex;
    align-items: center;
    gap: 2px;
    width: fit-content;
    margin-left: -2px;
    color: var(--fg-subtle);
    text-decoration: none;
    font-size: 13px;
  }
  .back:hover {
    color: var(--fg);
  }
  .row-main {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 16px;
    flex-wrap: wrap;
  }
  .titles {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
  }
  h1 {
    overflow-wrap: anywhere;
  }
  .meta {
    display: flex;
    align-items: center;
    gap: 12px;
    flex-wrap: wrap;
    margin-top: 4px;
  }
  .actions {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }
</style>
