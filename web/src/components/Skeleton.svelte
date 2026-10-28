<script lang="ts">
  let { rows = 3, variant = 'table' }: { rows?: number; variant?: 'table' | 'card' | 'lines' } = $props();
</script>

<div class="skeleton {variant}" aria-busy="true" aria-label="Loading">
  {#if variant === 'card'}
    <div class="grid">
      {#each Array(rows) as _, i (i)}
        <div class="block"></div>
      {/each}
    </div>
  {:else}
    {#each Array(rows) as _, i (i)}
      <div class="line" class:short={variant === 'lines' && i === rows - 1}></div>
    {/each}
  {/if}
</div>

<style>
  .skeleton {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .table {
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    padding: 14px;
  }
  .line,
  .block {
    background: var(--bg-muted);
    border-radius: var(--radius);
    animation: pulse 1.4s ease-in-out infinite;
  }
  .line {
    height: 14px;
  }
  .table .line {
    height: 28px;
  }
  .line.short {
    width: 60%;
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
    gap: 12px;
  }
  .block {
    height: 96px;
    border-radius: var(--radius-lg);
  }
</style>
