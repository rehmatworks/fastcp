<script lang="ts">
  import type { Component, Snippet } from 'svelte';
  import type { HTMLButtonAttributes } from 'svelte/elements';
  import Spinner from './Spinner.svelte';

  interface Props extends Omit<HTMLButtonAttributes, 'children'> {
    variant?: 'primary' | 'secondary' | 'ghost' | 'danger';
    size?: 'sm' | 'md';
    loading?: boolean;
    href?: string;
    external?: boolean;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    icon?: Component<any>;
    iconOnly?: boolean;
    full?: boolean;
    children?: Snippet;
  }

  let {
    variant = 'secondary',
    size = 'md',
    loading = false,
    href,
    external = false,
    icon: Icon,
    iconOnly = false,
    full = false,
    type = 'button',
    disabled,
    class: cls = '',
    children,
    ...rest
  }: Props = $props();

  const iconSize = $derived(size === 'sm' ? 14 : 16);
</script>

{#if href}
  <a
    {href}
    class="btn {variant} {size} {cls}"
    class:full
    class:icon-only={iconOnly}
    target={external ? '_blank' : undefined}
    rel={external ? 'noopener noreferrer external' : undefined}
    aria-label={rest['aria-label']}
    title={rest.title}
  >
    {#if Icon}<Icon size={iconSize} strokeWidth={1.75} aria-hidden="true" />{/if}
    {@render children?.()}
  </a>
{:else}
  <button
    {type}
    class="btn {variant} {size} {cls}"
    class:full
    class:icon-only={iconOnly}
    disabled={disabled || loading}
    aria-busy={loading || undefined}
    {...rest}
  >
    {#if loading}
      <Spinner size={iconSize} />
    {:else if Icon}
      <Icon size={iconSize} strokeWidth={1.75} aria-hidden="true" />
    {/if}
    {@render children?.()}
  </button>
{/if}

<style>
  .btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 7px;
    height: 34px;
    padding: 0 13px;
    border-radius: var(--radius);
    border: 1px solid var(--border-strong);
    background: var(--bg);
    color: var(--fg);
    font-weight: 500;
    font-size: 13.5px;
    line-height: 1;
    white-space: nowrap;
    text-decoration: none;
    cursor: pointer;
    user-select: none;
    transition:
      background 0.12s var(--ease),
      border-color 0.12s var(--ease),
      opacity 0.12s;
  }
  .btn:hover:not(:disabled) {
    background: var(--bg-muted);
  }
  .btn:active:not(:disabled) {
    background: var(--bg-emphasis);
  }
  .btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .btn[aria-busy='true'] {
    opacity: 0.75;
    cursor: progress;
  }
  .sm {
    height: 28px;
    padding: 0 10px;
    font-size: 12.5px;
    gap: 6px;
  }
  .primary,
  .danger {
    background: var(--bg-inverse);
    border-color: var(--bg-inverse);
    color: var(--fg-inverse);
  }
  .primary:hover:not(:disabled),
  .danger:hover:not(:disabled) {
    background: var(--bg-inverse);
    opacity: 0.86;
  }
  .primary:active:not(:disabled),
  .danger:active:not(:disabled) {
    background: var(--bg-inverse);
    opacity: 0.78;
  }
  .danger {
    text-decoration: underline;
    text-decoration-thickness: 1px;
    text-underline-offset: 3px;
  }
  .ghost {
    border-color: transparent;
    background: transparent;
    color: var(--fg-muted);
  }
  .ghost:hover:not(:disabled) {
    color: var(--fg);
  }
  .icon-only {
    width: 34px;
    padding: 0;
  }
  .icon-only.sm {
    width: 28px;
  }
  .full {
    width: 100%;
  }
</style>
