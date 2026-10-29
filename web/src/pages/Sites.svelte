<script lang="ts">
  import Plus from '@lucide/svelte/icons/plus';
  import Globe from '@lucide/svelte/icons/globe';
  import SearchX from '@lucide/svelte/icons/search-x';
  import PageHeader from '../components/PageHeader.svelte';
  import Button from '../components/Button.svelte';
  import SearchInput from '../components/SearchInput.svelte';
  import Skeleton from '../components/Skeleton.svelte';
  import ErrorState from '../components/ErrorState.svelte';
  import EmptyState from '../components/EmptyState.svelte';
  import Status from '../components/Status.svelte';
  import Badge from '../components/Badge.svelte';
  import NewSiteDrawer from './sites/NewSiteDrawer.svelte';
  import { api } from '../lib/api';
  import { Resource } from '../lib/page.svelte';
  import { router } from '../lib/router.svelte';
  import { session } from '../lib/session.svelte';
  import { date, siteType } from '../lib/format';
  import { siteSSL } from '../lib/status';
  import type { Site } from '../lib/types';

  const sites = new Resource((signal) => api.get<Site[]>('/sites', { signal }));
  let query = $state(router.query.get('q') ?? '');
  let creating = $state(router.query.get('new') === '1');

  $effect(() => router.setQuery({ q: query.trim() || null }));

  const filtered = $derived.by(() => {
    const q = query.trim().toLowerCase();
    const list = sites.data ?? [];
    if (!q) return list;
    return list.filter((s) => s.domains.some((d) => d.domain.includes(q)) || s.domain.includes(q));
  });

  const maxSites = $derived(session.user?.max_sites ?? -1);
  const atLimit = $derived(maxSites >= 0 && (sites.data?.length ?? 0) >= maxSites);
</script>

<PageHeader title="Sites" description="Websites served from this account.">
  {#snippet actions()}
    <Button
      variant="primary"
      icon={Plus}
      onclick={() => (creating = true)}
      disabled={atLimit}
      title={atLimit ? `This account is limited to ${maxSites} site${maxSites === 1 ? '' : 's'}.` : undefined}
    >
      New site
    </Button>
  {/snippet}
</PageHeader>

{#if sites.loading}
  <Skeleton rows={4} />
{:else if sites.error}
  <ErrorState message={sites.error} onretry={() => sites.reload()} />
{:else if (sites.data?.length ?? 0) === 0}
  <EmptyState icon={Globe} title="No sites yet" description="Create a site for a domain to start serving PHP or WordPress.">
    <Button variant="primary" icon={Plus} onclick={() => (creating = true)} disabled={atLimit}>New site</Button>
  </EmptyState>
{:else}
  <div class="stack">
    <div class="spread">
      <SearchInput bind:value={query} placeholder="Search domains" label="Search sites" />
      {#if atLimit}<p class="muted small">Site limit reached ({maxSites}).</p>{/if}
    </div>
    {#if filtered.length === 0}
      <EmptyState icon={SearchX} title="No matching sites" description="No site has a domain containing “{query.trim()}”." compact>
        <Button onclick={() => (query = '')}>Clear search</Button>
      </EmptyState>
    {:else}
      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr>
              <th>Site</th>
              <th>Type</th>
              <th>PHP</th>
              <th>HTTPS</th>
              <th>Created</th>
            </tr>
          </thead>
          <tbody>
            {#each filtered as site (site.id)}
              {@const ssl = siteSSL(site)}
              <tr class="clickable" onclick={() => router.navigate(`/sites/${site.id}`)}>
                <td class="primary-cell">
                  <a class="site-link cell-title" href="/sites/{site.id}" onclick={(e) => e.stopPropagation()}>{site.domain}</a>
                  {#if site.domains.length > 1}
                    <div class="cell-sub">+{site.domains.length - 1} more domain{site.domains.length > 2 ? 's' : ''}</div>
                  {/if}
                </td>
                <td data-label="Type">
                  <Badge>{siteType(site.site_type)}</Badge>
                </td>
                <td data-label="PHP" class="mono">{site.site_type === 'proxy' ? '—' : site.php_version}</td>
                <td data-label="HTTPS"><Status kind={ssl.kind} label={ssl.label} title={ssl.title} /></td>
                <td data-label="Created" class="muted nowrap">{date(site.created_at)}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  </div>
{/if}

<NewSiteDrawer
  open={creating}
  onclose={() => {
    creating = false;
    router.setQuery({ new: null });
  }}
/>

<style>
  .site-link {
    text-decoration: none;
    overflow-wrap: anywhere;
  }
  .site-link:hover {
    text-decoration: underline;
  }
</style>
