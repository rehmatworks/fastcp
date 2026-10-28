<script lang="ts">
  import type { Component } from 'svelte';
  import { onMount } from 'svelte';
  import Shell from './layout/Shell.svelte';
  import Toasts from './components/Toasts.svelte';
  import Spinner from './components/Spinner.svelte';
  import Login from './pages/Login.svelte';
  import Overview from './pages/Overview.svelte';
  import Sites from './pages/Sites.svelte';
  import SiteDetail from './pages/SiteDetail.svelte';
  import Databases from './pages/Databases.svelte';
  import Cron from './pages/Cron.svelte';
  import Backups from './pages/Backups.svelte';
  import Access from './pages/Access.svelte';
  import Users from './pages/Users.svelte';
  import Settings from './pages/Settings.svelte';
  import NotFound from './pages/NotFound.svelte';
  import { router, matchPath, type Params } from './lib/router.svelte';
  import { session } from './lib/session.svelte';

  interface Route {
    pattern: string;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    component: Component<any>;
    title: string;
    admin?: boolean;
    /** Parameters that identify the page; others (like tabs) keep it mounted. */
    key?: (p: Params) => string;
  }

  const routes: Route[] = [
    { pattern: '/', component: Overview, title: 'Overview' },
    { pattern: '/sites', component: Sites, title: 'Sites' },
    { pattern: '/sites/:id/:tab?', component: SiteDetail, title: 'Site', key: (p) => p.id },
    { pattern: '/databases', component: Databases, title: 'Databases' },
    { pattern: '/cron', component: Cron, title: 'Cron jobs' },
    { pattern: '/backups/:tab?', component: Backups, title: 'Backups', key: () => '' },
    { pattern: '/access', component: Access, title: 'SSH & SFTP' },
    { pattern: '/users', component: Users, title: 'Users', admin: true },
    { pattern: '/settings/:section?', component: Settings, title: 'Settings', admin: true, key: () => '' },
  ];

  const current = $derived.by(() => {
    for (const r of routes) {
      const params = matchPath(r.pattern, router.path);
      if (params) return { route: r, params };
    }
    return null;
  });

  const pageKey = $derived(
    `${session.user?.username}|${current?.route.pattern}|${current ? (current.route.key?.(current.params) ?? JSON.stringify(current.params)) : router.path}`,
  );

  $effect(() => {
    const title = router.path === '/login' ? 'Sign in' : (current?.route.title ?? 'Not found');
    document.title = `${title} · FastCP`;
  });

  $effect(() => {
    if (!session.ready) return;
    const onLogin = router.path === '/login';
    if (!session.me && !onLogin) {
      const here = router.path + router.search;
      router.navigate(`/login${here === '/' ? '' : `?next=${encodeURIComponent(here)}`}`, { replace: true, force: true });
    } else if (session.me && onLogin) {
      router.navigate('/', { replace: true, force: true });
    }
  });

  onMount(() => {
    router.start();
    void session.init();
  });
</script>

{#if !session.ready}
  <div class="boot"><Spinner size={20} /></div>
{:else if router.path === '/login'}
  <Login />
{:else if session.me}
  <Shell>
    {#key pageKey}
      {#if !current || (current.route.admin && !session.isAdmin)}
        <NotFound />
      {:else}
        <current.route.component params={current.params} />
      {/if}
    {/key}
  </Shell>
{/if}

<Toasts />

<style>
  .boot {
    min-height: 100vh;
    display: grid;
    place-items: center;
    color: var(--fg-subtle);
  }
</style>
