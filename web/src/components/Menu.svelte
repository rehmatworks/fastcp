<!--
  Action menu. The list is a popover in the top layer so it is never clipped
  by scrolling tables, and it closes on outside click or Escape.
-->
<script lang="ts" module>
  import type { Component } from 'svelte';

  export interface MenuItem {
    label: string;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    icon?: Component<any>;
    onclick: () => void;
    disabled?: boolean;
    destructive?: boolean;
    hidden?: boolean;
  }
</script>

<script lang="ts">
  import Ellipsis from '@lucide/svelte/icons/ellipsis';
  import { tick } from 'svelte';

  let { items, label = 'Actions' }: { items: MenuItem[]; label?: string } = $props();
  let btn: HTMLButtonElement | undefined = $state();
  let pop: HTMLDivElement | undefined = $state();
  let open = $state(false);
  let top = $state(0);
  let left = $state(0);
  const visible = $derived(items.filter((i) => !i.hidden));

  async function toggle() {
    if (!pop || !btn) return;
    if (open) {
      pop.hidePopover();
      return;
    }
    pop.showPopover();
    await tick();
    const r = btn.getBoundingClientRect();
    const w = pop.offsetWidth;
    const h = pop.offsetHeight;
    left = Math.max(8, Math.min(r.right - w, window.innerWidth - w - 8));
    top = r.bottom + 4 + h > window.innerHeight - 8 ? Math.max(8, r.top - h - 4) : r.bottom + 4;
    pop.querySelector<HTMLButtonElement>('button:not(:disabled)')?.focus();
  }

  function onkeydown(e: KeyboardEvent) {
    if (!pop) return;
    const buttons = [...pop.querySelectorAll<HTMLButtonElement>('button:not(:disabled)')];
    const i = buttons.indexOf(document.activeElement as HTMLButtonElement);
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      buttons[(i + 1) % buttons.length]?.focus();
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      buttons[(i - 1 + buttons.length) % buttons.length]?.focus();
    } else if (e.key === 'Home') {
      e.preventDefault();
      buttons[0]?.focus();
    } else if (e.key === 'End') {
      e.preventDefault();
      buttons[buttons.length - 1]?.focus();
    } else if (e.key === 'Tab') {
      pop.hidePopover();
    }
  }

  function choose(item: MenuItem) {
    pop?.hidePopover();
    item.onclick();
  }
</script>

<button
  bind:this={btn}
  type="button"
  class="trigger"
  aria-haspopup="menu"
  aria-expanded={open}
  aria-label={label}
  title={label}
  onclick={(e) => {
    e.stopPropagation();
    void toggle();
  }}
>
  <Ellipsis size={16} strokeWidth={1.75} aria-hidden="true" />
</button>

<div
  bind:this={pop}
  popover="auto"
  role="menu"
  tabindex="-1"
  class="menu"
  style:top="{top}px"
  style:left="{left}px"
  ontoggle={(e) => {
    open = (e as ToggleEvent).newState === 'open';
    if (!open && btn && pop?.contains(document.activeElement)) btn.focus();
  }}
  {onkeydown}
>
  {#each visible as item (item.label)}
    <button
      type="button"
      role="menuitem"
      class:destructive={item.destructive}
      disabled={item.disabled}
      onclick={(e) => {
        e.stopPropagation();
        choose(item);
      }}
    >
      {#if item.icon}<item.icon size={15} strokeWidth={1.75} aria-hidden="true" />{/if}
      {item.label}
    </button>
  {/each}
</div>

<style>
  .trigger {
    display: inline-grid;
    place-items: center;
    width: 30px;
    height: 30px;
    border: 1px solid transparent;
    border-radius: var(--radius);
    background: transparent;
    color: var(--fg-subtle);
    cursor: pointer;
  }
  .trigger:hover,
  .trigger[aria-expanded='true'] {
    border-color: var(--border);
    background: var(--bg-muted);
    color: var(--fg);
  }
  .menu {
    position: fixed;
    inset: auto;
    margin: 0;
    min-width: 180px;
    padding: 4px;
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-lg);
    background: var(--bg);
    color: var(--fg);
    box-shadow: var(--shadow);
    animation: pop-in 0.12s var(--ease);
  }
  .menu button {
    display: flex;
    align-items: center;
    gap: 9px;
    width: 100%;
    padding: 7px 10px;
    border: 0;
    border-radius: var(--radius);
    background: transparent;
    text-align: left;
    font-size: 13.5px;
    cursor: pointer;
    white-space: nowrap;
  }
  .menu button:hover:not(:disabled),
  .menu button:focus-visible {
    background: var(--bg-muted);
    outline: none;
  }
  .menu button:disabled {
    color: var(--fg-faint);
    cursor: not-allowed;
  }
  .menu button.destructive {
    font-weight: 500;
  }
  .menu button.destructive:not(:first-child) {
    margin-top: 4px;
    position: relative;
  }
  .menu button.destructive:not(:first-child)::before {
    content: '';
    position: absolute;
    top: -3px;
    left: 6px;
    right: 6px;
    height: 1px;
    background: var(--border);
  }
</style>
