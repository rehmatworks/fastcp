<script lang="ts">
  import type { Snippet } from 'svelte';

  interface Props {
    title?: string;
    description?: string;
    actions?: Snippet;
    footer?: Snippet;
    children: Snippet;
    flush?: boolean;
    id?: string;
  }
  let { title, description, actions, footer, children, flush = false, id }: Props = $props();
</script>

<section class="card" {id} aria-label={title}>
  {#if title || actions}
    <header>
      <div class="titles">
        {#if title}<h2>{title}</h2>{/if}
        {#if description}<p class="muted">{description}</p>{/if}
      </div>
      {#if actions}<div class="actions">{@render actions()}</div>{/if}
    </header>
  {/if}
  <div class="content" class:flush>{@render children()}</div>
  {#if footer}<footer>{@render footer()}</footer>{/if}
</section>

<style>
  .card {
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    background: var(--bg);
    min-width: 0;
  }
  header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 12px;
    padding: 16px 18px 0;
  }
  .titles {
    display: flex;
    flex-direction: column;
    gap: 3px;
    min-width: 0;
    flex: 1;
  }
  .titles p {
    font-size: 13px;
  }
  .actions {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-shrink: 0;
  }
  .content {
    padding: 16px 18px 18px;
  }
  .content.flush {
    padding: 12px 0 0;
  }
  footer {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 8px;
    padding: 12px 18px;
    border-top: 1px solid var(--border);
    background: var(--bg-subtle);
    border-radius: 0 0 var(--radius-lg) var(--radius-lg);
    flex-wrap: wrap;
  }
</style>
