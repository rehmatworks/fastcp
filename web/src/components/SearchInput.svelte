<script lang="ts">
  import Search from '@lucide/svelte/icons/search';
  import X from '@lucide/svelte/icons/x';

  let { value = $bindable(''), placeholder = 'Search', label = 'Search' }: { value?: string; placeholder?: string; label?: string } =
    $props();
</script>

<div class="search">
  <Search size={15} strokeWidth={1.75} aria-hidden="true" class="icon" />
  <input type="search" bind:value {placeholder} aria-label={label} autocomplete="off" spellcheck="false" />
  {#if value}
    <button type="button" aria-label="Clear search" onclick={() => (value = '')}>
      <X size={14} strokeWidth={1.75} aria-hidden="true" />
    </button>
  {/if}
</div>

<style>
  .search {
    position: relative;
    display: flex;
    align-items: center;
    width: min(280px, 100%);
  }
  .search :global(.icon) {
    position: absolute;
    left: 10px;
    color: var(--fg-subtle);
    pointer-events: none;
  }
  input {
    width: 100%;
    height: 34px;
    padding: 0 32px 0 32px;
    border: 1px solid var(--border-strong);
    border-radius: var(--radius);
    background: var(--bg);
    font-size: 13.5px;
  }
  input::-webkit-search-cancel-button {
    display: none;
  }
  input:focus-visible {
    outline: none;
    border-color: var(--fg);
    box-shadow: 0 0 0 1px var(--fg);
  }
  button {
    position: absolute;
    right: 5px;
    display: grid;
    place-items: center;
    width: 24px;
    height: 24px;
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
