<script lang="ts" generics="T extends string">
  import type { Component } from 'svelte';

  interface Option {
    value: T;
    label: string;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    icon?: Component<any>;
  }
  interface Props {
    value: T;
    options: Option[];
    label: string;
    showLabel?: boolean;
    full?: boolean;
    onchange?: (value: T) => void;
  }
  let { value = $bindable(), options, label, showLabel = false, full = false, onchange }: Props = $props();
  const uid = $props.id();

  function select(v: T) {
    value = v;
    onchange?.(v);
  }

  function onkeydown(e: KeyboardEvent) {
    const i = options.findIndex((o) => o.value === value);
    let next = -1;
    if (e.key === 'ArrowRight' || e.key === 'ArrowDown') next = (i + 1) % options.length;
    if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') next = (i - 1 + options.length) % options.length;
    if (next < 0) return;
    e.preventDefault();
    select(options[next].value);
    (e.currentTarget as HTMLElement).querySelectorAll<HTMLButtonElement>('button')[next]?.focus();
  }
</script>

<div class="wrap">
  {#if showLabel}<span class="label" id="seg-{uid}">{label}</span>{/if}
  <div
    class="segmented"
    class:full
    role="radiogroup"
    tabindex="-1"
    aria-label={showLabel ? undefined : label}
    aria-labelledby={showLabel ? `seg-${uid}` : undefined}
    {onkeydown}
  >
    {#each options as o (o.value)}
      <button
        type="button"
        role="radio"
        aria-checked={o.value === value}
        tabindex={o.value === value ? 0 : -1}
        onclick={() => select(o.value)}
      >
        {#if o.icon}<o.icon size={14} strokeWidth={1.75} aria-hidden="true" />{/if}
        {o.label}
      </button>
    {/each}
  </div>
</div>

<style>
  .wrap {
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-width: 0;
  }
  .label {
    font-weight: 500;
    font-size: 13px;
  }
  .segmented {
    display: inline-flex;
    padding: 2px;
    gap: 2px;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--bg-muted);
    width: fit-content;
    max-width: 100%;
    overflow-x: auto;
  }
  .segmented.full {
    display: flex;
    width: 100%;
  }
  .full button {
    flex: 1;
  }
  button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    height: 28px;
    padding: 0 12px;
    border: 1px solid transparent;
    border-radius: calc(var(--radius) - 2px);
    background: transparent;
    color: var(--fg-subtle);
    font-size: 13px;
    font-weight: 500;
    cursor: pointer;
    white-space: nowrap;
  }
  button:hover {
    color: var(--fg);
  }
  button[aria-checked='true'] {
    background: var(--bg);
    border-color: var(--border);
    color: var(--fg);
    box-shadow: var(--shadow-sm);
  }
</style>
