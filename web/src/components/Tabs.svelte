<!-- Navigation tabs rendered as links, so each section has its own URL. -->
<script lang="ts">
  interface Tab {
    id: string;
    label: string;
    href: string;
  }
  let { tabs, active, label = 'Sections' }: { tabs: Tab[]; active: string; label?: string } = $props();
</script>

<nav class="tabs" aria-label={label}>
  {#each tabs as t (t.id)}
    <a href={t.href} class:active={t.id === active} aria-current={t.id === active ? 'page' : undefined}>{t.label}</a>
  {/each}
</nav>

<style>
  .tabs {
    display: flex;
    gap: 4px;
    border-bottom: 1px solid var(--border);
    overflow-x: auto;
    scrollbar-width: none;
  }
  .tabs::-webkit-scrollbar {
    display: none;
  }
  a {
    position: relative;
    padding: 9px 10px 11px;
    color: var(--fg-subtle);
    text-decoration: none;
    font-weight: 500;
    font-size: 13.5px;
    white-space: nowrap;
    border-radius: var(--radius) var(--radius) 0 0;
  }
  a:hover {
    color: var(--fg);
  }
  a.active {
    color: var(--fg);
  }
  a.active::after {
    content: '';
    position: absolute;
    left: 8px;
    right: 8px;
    bottom: -1px;
    height: 2px;
    background: var(--fg);
    border-radius: 2px;
  }
</style>
