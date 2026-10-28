<script lang="ts">
  import ChevronLeft from '@lucide/svelte/icons/chevron-left';
  import ChevronRight from '@lucide/svelte/icons/chevron-right';
  import Button from './Button.svelte';

  let { page = $bindable(1), total, perPage }: { page?: number; total: number; perPage: number } = $props();
  const pages = $derived(Math.max(1, Math.ceil(total / perPage)));
  const from = $derived(total === 0 ? 0 : (page - 1) * perPage + 1);
  const to = $derived(Math.min(total, page * perPage));

  $effect(() => {
    if (page > pages) page = pages;
  });
</script>

{#if total > perPage}
  <nav class="pagination" aria-label="Pagination">
    <span class="muted small">Showing {from}–{to} of {total}</span>
    <div class="row">
      <Button size="sm" icon={ChevronLeft} disabled={page <= 1} onclick={() => (page -= 1)}>Previous</Button>
      <Button size="sm" disabled={page >= pages} onclick={() => (page += 1)}>
        Next <ChevronRight size={14} strokeWidth={1.75} aria-hidden="true" />
      </Button>
    </div>
  </nav>
{/if}

<style>
  .pagination {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 10px 14px;
    border-top: 1px solid var(--border);
    flex-wrap: wrap;
  }
</style>
