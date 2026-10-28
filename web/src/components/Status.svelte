<!-- Status shown as icon plus text, never by color alone. -->
<script lang="ts">
  import CircleCheck from '@lucide/svelte/icons/circle-check';
  import CircleX from '@lucide/svelte/icons/circle-x';
  import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
  import Clock from '@lucide/svelte/icons/clock';
  import CircleDashed from '@lucide/svelte/icons/circle-dashed';
  import Info from '@lucide/svelte/icons/info';
  import Spinner from './Spinner.svelte';
  import type { StatusKind } from '../lib/status';

  let { kind, label, title }: { kind: StatusKind; label: string; title?: string } = $props();
  const icons = { ok: CircleCheck, error: CircleX, warn: TriangleAlert, pending: Clock, off: CircleDashed, info: Info };
</script>

<span class="status {kind}" {title}>
  {#if kind === 'running'}
    <Spinner size={14} label="" />
  {:else}
    {@const Icon = icons[kind]}
    <Icon size={14} strokeWidth={kind === 'error' || kind === 'warn' ? 2.25 : 1.75} aria-hidden="true" />
  {/if}
  <span>{label}</span>
</span>

<style>
  .status {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 13px;
    white-space: nowrap;
    color: var(--fg-muted);
  }
  .status :global(svg) {
    flex-shrink: 0;
  }
  .ok,
  .error,
  .warn {
    color: var(--fg);
  }
  .error span,
  .warn span {
    font-weight: 500;
  }
  .off {
    color: var(--fg-subtle);
  }
</style>
