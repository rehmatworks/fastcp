<script lang="ts">
  import type { Snippet } from 'svelte';
  let { children, strong = false, title }: { children: Snippet; strong?: boolean; title?: string } = $props();
</script>

<span class="badge" class:strong {title}>{@render children()}</span>

<style>
  .badge {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    height: 20px;
    padding: 0 7px;
    border: 1px solid var(--border-strong);
    border-radius: 999px;
    font-size: 11.5px;
    font-weight: 500;
    color: var(--fg-muted);
    white-space: nowrap;
    vertical-align: middle;
  }
  .strong {
    background: var(--bg-inverse);
    border-color: var(--bg-inverse);
    color: var(--fg-inverse);
  }
</style>
