<script lang="ts">
  import { tick } from 'svelte';
  import ChevronsUpDown from '@lucide/svelte/icons/chevrons-up-down';
  import KeyRound from '@lucide/svelte/icons/key-round';
  import LogOut from '@lucide/svelte/icons/log-out';
  import Monitor from '@lucide/svelte/icons/monitor';
  import Sun from '@lucide/svelte/icons/sun';
  import Moon from '@lucide/svelte/icons/moon';
  import Segmented from '../components/Segmented.svelte';
  import ChangePasswordDialog from './ChangePasswordDialog.svelte';
  import { session } from '../lib/session.svelte';
  import { theme, type ThemeChoice } from '../lib/theme.svelte';

  let btn: HTMLButtonElement | undefined = $state();
  let pop: HTMLDivElement | undefined = $state();
  let open = $state(false);
  let bottom = $state(0);
  let left = $state(0);
  let width = $state(220);
  let pwOpen = $state(false);

  const me = $derived(session.me);
  const initial = $derived((me?.actor.username ?? '?').slice(0, 1).toUpperCase());

  async function toggle() {
    if (!pop || !btn) return;
    if (open) return pop.hidePopover();
    pop.showPopover();
    await tick();
    const r = btn.getBoundingClientRect();
    width = Math.max(220, r.width);
    left = r.left;
    bottom = window.innerHeight - r.top + 6;
  }

  function close() {
    pop?.hidePopover();
  }
</script>

<button
  bind:this={btn}
  type="button"
  class="account"
  aria-haspopup="true"
  aria-expanded={open}
  onclick={toggle}
>
  <span class="avatar" aria-hidden="true">{initial}</span>
  <span class="who">
    <span class="name truncate">{me?.actor.username}</span>
    <span class="role">{me?.actor.is_admin ? 'Administrator' : 'User'}</span>
  </span>
  <ChevronsUpDown size={14} strokeWidth={1.75} aria-hidden="true" />
</button>

<div
  bind:this={pop}
  popover="auto"
  class="panel"
  style:left="{left}px"
  style:bottom="{bottom}px"
  style:width="{width}px"
  ontoggle={(e) => (open = (e as ToggleEvent).newState === 'open')}
>
  <div class="section">
    <Segmented
      label="Theme"
      full
      value={theme.choice}
      onchange={(v: ThemeChoice) => theme.set(v)}
      options={[
        { value: 'system', label: 'Auto', icon: Monitor },
        { value: 'light', label: 'Light', icon: Sun },
        { value: 'dark', label: 'Dark', icon: Moon },
      ]}
    />
  </div>
  <div class="items">
    {#if !me?.impersonating}
      <button
        type="button"
        onclick={() => {
          close();
          pwOpen = true;
        }}
      >
        <KeyRound size={15} strokeWidth={1.75} aria-hidden="true" /> Change password
      </button>
    {/if}
    <button
      type="button"
      onclick={() => {
        close();
        void session.logout();
      }}
    >
      <LogOut size={15} strokeWidth={1.75} aria-hidden="true" /> Sign out
    </button>
  </div>
</div>

<ChangePasswordDialog open={pwOpen} onclose={() => (pwOpen = false)} />

<style>
  .account {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    padding: 8px;
    border: 1px solid transparent;
    border-radius: var(--radius);
    background: transparent;
    text-align: left;
    cursor: pointer;
  }
  .account:hover,
  .account[aria-expanded='true'] {
    background: var(--bg-muted);
    border-color: var(--border);
  }
  .avatar {
    display: grid;
    place-items: center;
    width: 28px;
    height: 28px;
    flex-shrink: 0;
    border-radius: 50%;
    background: var(--bg-inverse);
    color: var(--fg-inverse);
    font-size: 12px;
    font-weight: 600;
  }
  .who {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-width: 0;
    line-height: 1.25;
  }
  .name {
    font-weight: 500;
    font-size: 13px;
  }
  .role {
    font-size: 11.5px;
    color: var(--fg-subtle);
  }
  .panel {
    position: fixed;
    inset: auto;
    margin: 0;
    padding: 0;
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-lg);
    background: var(--bg);
    color: var(--fg);
    box-shadow: var(--shadow);
    animation: pop-in 0.12s var(--ease);
  }
  .section {
    padding: 10px;
    border-bottom: 1px solid var(--border);
  }
  .items {
    padding: 4px;
  }
  .items button {
    display: flex;
    align-items: center;
    gap: 9px;
    width: 100%;
    padding: 7px 10px;
    border: 0;
    border-radius: var(--radius);
    background: transparent;
    font-size: 13.5px;
    cursor: pointer;
  }
  .items button:hover,
  .items button:focus-visible {
    background: var(--bg-muted);
    outline: none;
  }
</style>
