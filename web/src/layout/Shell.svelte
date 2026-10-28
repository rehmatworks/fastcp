<script lang="ts">
  import type { Snippet } from 'svelte';
  import LayoutGrid from '@lucide/svelte/icons/layout-grid';
  import Globe from '@lucide/svelte/icons/globe';
  import DatabaseIcon from '@lucide/svelte/icons/database';
  import CalendarClock from '@lucide/svelte/icons/calendar-clock';
  import Archive from '@lucide/svelte/icons/archive';
  import KeyRound from '@lucide/svelte/icons/key-round';
  import UsersIcon from '@lucide/svelte/icons/users';
  import SettingsIcon from '@lucide/svelte/icons/settings';
  import Menu from '@lucide/svelte/icons/menu';
  import X from '@lucide/svelte/icons/x';
  import UserRoundCheck from '@lucide/svelte/icons/user-round-check';
  import Logo from '../components/Logo.svelte';
  import Button from '../components/Button.svelte';
  import AccountMenu from './AccountMenu.svelte';
  import { router, isActive } from '../lib/router.svelte';
  import { session } from '../lib/session.svelte';
  import { errorMessage } from '../lib/api';
  import { toast } from '../lib/toast.svelte';

  let { children }: { children: Snippet } = $props();
  let navOpen = $state(false);
  let stopping = $state(false);

  const main = [
    { href: '/', label: 'Overview', icon: LayoutGrid },
    { href: '/sites', label: 'Sites', icon: Globe },
    { href: '/databases', label: 'Databases', icon: DatabaseIcon },
    { href: '/cron', label: 'Cron jobs', icon: CalendarClock },
    { href: '/backups', label: 'Backups', icon: Archive },
    { href: '/access', label: 'SSH & SFTP', icon: KeyRound },
  ];
  const admin = [
    { href: '/users', label: 'Users', icon: UsersIcon },
    { href: '/settings', label: 'Settings', icon: SettingsIcon },
  ];

  $effect(() => {
    void router.path;
    navOpen = false;
  });

  async function stopActing() {
    stopping = true;
    try {
      await session.stopImpersonation();
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      stopping = false;
    }
  }

  const actingUntil = $derived(
    session.me?.acting_until
      ? new Date(session.me.acting_until).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
      : '',
  );
</script>

<a class="skip" href="#main">Skip to content</a>

<div class="shell">
  <header class="topbar">
    <button type="button" class="nav-toggle" aria-label="Open navigation" aria-expanded={navOpen} onclick={() => (navOpen = true)}>
      <Menu size={18} strokeWidth={1.75} aria-hidden="true" />
    </button>
    <a href="/" class="brand" aria-label="FastCP overview"><Logo size={22} /></a>
  </header>

  {#if navOpen}
    <button type="button" class="scrim" aria-label="Close navigation" onclick={() => (navOpen = false)}></button>
  {/if}

  <aside class="sidebar" class:open={navOpen} aria-label="Main navigation">
    <div class="side-head">
      <a href="/" class="brand"><Logo size={22} /></a>
      <button type="button" class="nav-close" aria-label="Close navigation" onclick={() => (navOpen = false)}>
        <X size={18} strokeWidth={1.75} aria-hidden="true" />
      </button>
    </div>
    {#if session.meta?.hostname}
      <p class="host truncate" title={session.meta.hostname}>{session.meta.hostname}</p>
    {/if}
    <nav>
      <ul>
        {#each main as item (item.href)}
          <li>
            <a href={item.href} class:active={isActive(item.href, router.path)} aria-current={isActive(item.href, router.path) ? 'page' : undefined}>
              <item.icon size={16} strokeWidth={1.75} aria-hidden="true" />
              {item.label}
            </a>
          </li>
        {/each}
      </ul>
      {#if session.isAdmin}
        <p class="group">Administration</p>
        <ul>
          {#each admin as item (item.href)}
            <li>
              <a href={item.href} class:active={isActive(item.href, router.path)} aria-current={isActive(item.href, router.path) ? 'page' : undefined}>
                <item.icon size={16} strokeWidth={1.75} aria-hidden="true" />
                {item.label}
              </a>
            </li>
          {/each}
        </ul>
      {/if}
    </nav>
    <div class="side-foot">
      <AccountMenu />
    </div>
  </aside>

  <div class="main-col">
    {#if session.me?.impersonating}
      <div class="acting" role="status">
        <UserRoundCheck size={16} strokeWidth={1.75} aria-hidden="true" />
        <p>
          Acting as <strong>{session.me.user.username}</strong>{#if actingUntil}{' '}<span class="muted">until {actingUntil}</span>{/if}.
          Changes you make apply to this account.
        </p>
        <Button size="sm" variant="primary" loading={stopping} onclick={stopActing}>Return to {session.me.actor.username}</Button>
      </div>
    {/if}
    <main id="main" tabindex="-1">
      {@render children()}
    </main>
  </div>
</div>

<style>
  .skip {
    position: absolute;
    left: 8px;
    top: -40px;
    z-index: 50;
    padding: 8px 12px;
    background: var(--bg-inverse);
    color: var(--fg-inverse);
    border-radius: var(--radius);
  }
  .skip:focus {
    top: 8px;
  }
  .shell {
    display: grid;
    grid-template-columns: var(--sidebar) minmax(0, 1fr);
    min-height: 100vh;
  }
  .topbar {
    display: none;
  }
  .sidebar {
    position: sticky;
    top: 0;
    height: 100vh;
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 16px 12px 12px;
    border-right: 1px solid var(--border);
    background: var(--bg-subtle);
    overflow-y: auto;
  }
  .side-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 2px 8px 0;
  }
  .brand {
    color: inherit;
    text-decoration: none;
    border-radius: var(--radius);
  }
  .host {
    padding: 2px 8px 10px;
    font-size: 12px;
    color: var(--fg-subtle);
    font-family: var(--mono);
  }
  .nav-close {
    display: none;
  }
  nav {
    flex: 1;
    margin-top: 8px;
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  nav a {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 7px 10px;
    border-radius: var(--radius);
    color: var(--fg-muted);
    text-decoration: none;
    font-weight: 500;
    font-size: 13.5px;
    border: 1px solid transparent;
  }
  nav a:hover {
    color: var(--fg);
    background: var(--bg-muted);
  }
  nav a.active {
    color: var(--fg);
    background: var(--bg);
    border-color: var(--border);
    box-shadow: var(--shadow-sm);
  }
  .group {
    margin: 18px 10px 6px;
    font-size: 11.5px;
    font-weight: 500;
    color: var(--fg-subtle);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .side-foot {
    border-top: 1px solid var(--border);
    padding-top: 10px;
  }
  .main-col {
    min-width: 0;
    display: flex;
    flex-direction: column;
  }
  .acting {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 32px;
    border-bottom: 1px solid var(--border-strong);
    background: var(--bg-muted);
    flex-wrap: wrap;
  }
  .acting p {
    flex: 1;
    min-width: 200px;
    font-size: 13.5px;
  }
  main {
    width: 100%;
    max-width: calc(var(--content) + 64px);
    padding: 32px 32px 64px;
    outline: none;
  }
  .scrim {
    display: none;
  }

  @media (max-width: 900px) {
    .shell {
      grid-template-columns: minmax(0, 1fr);
    }
    .topbar {
      position: sticky;
      top: 0;
      z-index: 20;
      display: flex;
      align-items: center;
      gap: 8px;
      height: 52px;
      padding: 0 12px;
      border-bottom: 1px solid var(--border);
      background: var(--bg);
    }
    .nav-toggle,
    .nav-close {
      display: grid;
      place-items: center;
      width: 34px;
      height: 34px;
      border: 0;
      border-radius: var(--radius);
      background: transparent;
      cursor: pointer;
    }
    .nav-toggle:hover,
    .nav-close:hover {
      background: var(--bg-muted);
    }
    .sidebar {
      position: fixed;
      inset: 0 auto 0 0;
      z-index: 40;
      width: min(280px, 86vw);
      transform: translateX(-100%);
      transition: transform 0.2s var(--ease);
      box-shadow: var(--shadow);
      visibility: hidden;
    }
    .sidebar.open {
      transform: none;
      visibility: visible;
    }
    .scrim {
      display: block;
      position: fixed;
      inset: 0;
      z-index: 30;
      border: 0;
      background: var(--overlay);
      animation: fade-in 0.15s var(--ease);
    }
    main {
      padding: 20px 16px 48px;
    }
    .acting {
      padding: 10px 16px;
    }
  }
</style>
