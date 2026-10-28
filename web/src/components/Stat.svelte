<script lang="ts">
  import type { Component } from 'svelte';
  import ArrowUpRight from '@lucide/svelte/icons/arrow-up-right';

  interface Props {
    label: string;
    value: string | number;
    sub?: string;
    href?: string;
    meter?: number;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    icon?: Component<any>;
  }
  let { label, value, sub, href, meter, icon: Icon }: Props = $props();
</script>

<svelte:element this={href ? 'a' : 'div'} {href} class="stat" class:link={!!href}>
  <div class="top">
    <span class="label">
      {#if Icon}<Icon size={14} strokeWidth={1.75} aria-hidden="true" />{/if}
      {label}
    </span>
    {#if href}<ArrowUpRight size={14} strokeWidth={1.75} aria-hidden="true" class="arrow" />{/if}
  </div>
  <div class="value">{value}</div>
  {#if meter !== undefined}
    <div class="meter" role="meter" aria-valuenow={meter} aria-valuemin={0} aria-valuemax={100} aria-label="{label} used">
      <span style:width="{meter}%"></span>
    </div>
  {/if}
  {#if sub}<div class="sub">{sub}</div>{/if}
</svelte:element>

<style>
  .stat {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 14px 16px;
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    background: var(--bg);
    color: inherit;
    text-decoration: none;
    min-width: 0;
  }
  .stat.link:hover {
    border-color: var(--border-strong);
    background: var(--bg-subtle);
  }
  .top {
    display: flex;
    align-items: center;
    justify-content: space-between;
    color: var(--fg-subtle);
  }
  .label {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 12.5px;
    font-weight: 500;
  }
  .value {
    font-size: 22px;
    font-weight: 600;
    letter-spacing: -0.02em;
    line-height: 1.2;
    font-variant-numeric: tabular-nums;
    overflow-wrap: anywhere;
  }
  .sub {
    font-size: 12.5px;
    color: var(--fg-subtle);
  }
  .stat :global(.arrow) {
    opacity: 0;
    transition: opacity 0.12s;
  }
  .stat.link:hover :global(.arrow) {
    opacity: 1;
  }
</style>
