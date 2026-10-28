<script lang="ts">
  import PageHeader from '../components/PageHeader.svelte';
  import Tabs from '../components/Tabs.svelte';
  import PHPSection from './settings/PHPSection.svelte';
  import WebServerSection from './settings/WebServerSection.svelte';
  import MySQLSection from './settings/MySQLSection.svelte';
  import SecuritySection from './settings/SecuritySection.svelte';
  import UpdatesSection from './settings/UpdatesSection.svelte';

  let { params }: { params: { section?: string } } = $props();

  const tabs = [
    { id: 'php', label: 'PHP', href: '/settings' },
    { id: 'webserver', label: 'Web server', href: '/settings/webserver' },
    { id: 'mysql', label: 'MySQL', href: '/settings/mysql' },
    { id: 'security', label: 'SSH & firewall', href: '/settings/security' },
    { id: 'updates', label: 'Updates', href: '/settings/updates' },
  ];
  const active = $derived(tabs.some((t) => t.id === params.section) ? params.section! : 'php');
</script>

<PageHeader title="Settings" description="Server-wide configuration. Changes here affect every account." />

<Tabs {tabs} {active} label="Settings sections" />

<div class="section">
  {#key active}
    {#if active === 'php'}
      <PHPSection />
    {:else if active === 'webserver'}
      <WebServerSection />
    {:else if active === 'mysql'}
      <MySQLSection />
    {:else if active === 'security'}
      <SecuritySection />
    {:else}
      <UpdatesSection />
    {/if}
  {/key}
</div>

<style>
  .section {
    margin-top: 24px;
  }
</style>
