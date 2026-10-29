<script lang="ts">
  import ExternalLink from '@lucide/svelte/icons/external-link';
  import PageHeader from '../components/PageHeader.svelte';
  import Tabs from '../components/Tabs.svelte';
  import Skeleton from '../components/Skeleton.svelte';
  import ErrorState from '../components/ErrorState.svelte';
  import Button from '../components/Button.svelte';
  import Badge from '../components/Badge.svelte';
  import Status from '../components/Status.svelte';
  import NotFound from './NotFound.svelte';
  import SiteOverview from './sites/SiteOverview.svelte';
  import SiteDomains from './sites/SiteDomains.svelte';
  import SitePHP from './sites/SitePHP.svelte';
  import SiteProxy from './sites/SiteProxy.svelte';
  import SiteCaching from './sites/SiteCaching.svelte';
  import SiteLogs from './sites/SiteLogs.svelte';
  import SiteBackups from './sites/SiteBackups.svelte';
  import SiteDanger from './sites/SiteDanger.svelte';
  import { api, seg } from '../lib/api';
  import { Resource } from '../lib/page.svelte';
  import { siteSSL } from '../lib/status';
  import { siteType } from '../lib/format';
  import type { Site } from '../lib/types';

  let { params }: { params: { id: string; tab?: string } } = $props();

  const site = new Resource((signal) => api.get<Site>(`/sites/${seg(params.id)}`, { signal }));

  const proxy = $derived(site.data?.site_type === 'proxy');
  const tabs = $derived([
    { id: 'overview', label: 'Overview', href: `/sites/${params.id}` },
    { id: 'domains', label: 'Domains & HTTPS', href: `/sites/${params.id}/domains` },
    proxy ? { id: 'proxy', label: 'Proxy', href: `/sites/${params.id}/proxy` } : { id: 'php', label: 'PHP', href: `/sites/${params.id}/php` },
    { id: 'caching', label: 'Caching', href: `/sites/${params.id}/caching` },
    { id: 'logs', label: 'Logs', href: `/sites/${params.id}/logs` },
    { id: 'backups', label: 'Backups', href: `/sites/${params.id}/backups` },
    { id: 'danger', label: 'Danger zone', href: `/sites/${params.id}/danger` },
  ]);
  const active = $derived(tabs.some((t) => t.id === params.tab) ? params.tab! : 'overview');
  const notFound = $derived(site.error !== null && site.data === undefined && site.status === 404);

  function update(next: Site) {
    site.set(next);
  }
</script>

{#if site.loading}
  <Skeleton variant="lines" rows={3} />
  <div class="gap"><Skeleton rows={4} /></div>
{:else if notFound}
  <NotFound />
{:else if site.error && !site.data}
  <ErrorState message={site.error} onretry={() => site.reload()} />
{:else if site.data}
  {@const s = site.data}
  {@const ssl = siteSSL(s)}
  <PageHeader title={s.domain} back={{ href: '/sites', label: 'Sites' }}>
    {#snippet meta()}
      <Badge>{siteType(s.site_type)}</Badge>
      <span class="mono small muted">{s.site_type === 'proxy' ? s.proxy_url : `PHP ${s.php_version}`}</span>
      <Status kind={ssl.kind} label={ssl.label} title={ssl.title} />
    {/snippet}
    {#snippet actions()}
      <Button href="{s.force_https || ssl.kind === 'ok' ? 'https' : 'http'}://{s.domain}/" external icon={ExternalLink}>Visit site</Button>
    {/snippet}
  </PageHeader>

  <Tabs {tabs} {active} label="Site sections" />

  <div class="section">
    {#if active === 'overview'}
      <SiteOverview site={s} />
    {:else if active === 'domains'}
      <SiteDomains site={s} onchange={update} />
    {:else if active === 'php'}
      <SitePHP site={s} onchange={update} />
    {:else if active === 'proxy'}
      <SiteProxy site={s} onchange={update} />
    {:else if active === 'caching'}
      <SiteCaching site={s} onchange={update} />
    {:else if active === 'logs'}
      <SiteLogs site={s} />
    {:else if active === 'backups'}
      <SiteBackups site={s} />
    {:else if active === 'danger'}
      <SiteDanger site={s} />
    {/if}
  </div>
{/if}

<style>
  .section {
    margin-top: 24px;
  }
  .gap {
    margin-top: 24px;
  }
</style>
