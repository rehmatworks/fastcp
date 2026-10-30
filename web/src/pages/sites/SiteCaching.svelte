<script lang="ts">
  import RefreshCw from '@lucide/svelte/icons/refresh-cw';
  import Card from '../../components/Card.svelte';
  import Input from '../../components/Input.svelte';
  import Switch from '../../components/Switch.svelte';
  import Checkbox from '../../components/Checkbox.svelte';
  import Button from '../../components/Button.svelte';
  import SaveBar from '../../components/SaveBar.svelte';
  import Status from '../../components/Status.svelte';
  import { api, errorMessage, seg } from '../../lib/api';
  import { Form, Resource } from '../../lib/page.svelte';
  import { toast } from '../../lib/toast.svelte';
  import type { PageCacheStatus, Site } from '../../lib/types';

  let { site, onchange }: { site: Site; onchange: (s: Site) => void } = $props();

  function pick(s: Site) {
    return {
      compression_enabled: s.compression_enabled,
      gzip_enabled: s.gzip_enabled,
      zstd_enabled: s.zstd_enabled,
      cache_control_enabled: s.cache_control_enabled,
      cache_control_value: s.cache_control_value || 'public, max-age=3600',
      page_cache: s.page_cache,
    };
  }

  // svelte-ignore state_referenced_locally
  const form = new Form(pick(site));
  let error = $state<string | null>(null);

  const pageCache = new Resource((signal) =>
    site.site_type === 'proxy'
      ? Promise.resolve<PageCacheStatus>({ available: false, serving: false })
      : api.get<PageCacheStatus>(`/sites/${seg(site.id)}/page-cache`, { signal }),
  );
  let rechecking = $state(false);

  async function recheck() {
    rechecking = true;
    await pageCache.reload({ quiet: true });
    rechecking = false;
  }

  function pluginName(pc: PageCacheStatus) {
    return pc.version ? `${pc.plugin} ${pc.version}` : (pc.plugin ?? '');
  }

  const presets = [
    { label: '1 hour', value: 'public, max-age=3600' },
    { label: '1 day', value: 'public, max-age=86400' },
    { label: '1 week', value: 'public, max-age=604800' },
    { label: 'No cache', value: 'no-cache' },
  ];

  const encoderErr = $derived(
    form.values.compression_enabled && !form.values.gzip_enabled && !form.values.zstd_enabled ? 'Choose at least one encoding.' : null,
  );

  async function save(e: Event) {
    e.preventDefault();
    if (encoderErr) return;
    form.saving = true;
    error = null;
    try {
      const next = await api.patch<Site>(`/sites/${seg(site.id)}`, form.values);
      form.commit(pick(next));
      onchange(next);
      toast.success('Caching settings saved.');
    } catch (err) {
      error = errorMessage(err);
    } finally {
      form.saving = false;
    }
  }
</script>

<Card
  title="Caching & compression"
  description={site.site_type === 'proxy' ? 'Applied by the web server in front of your application.' : 'Applied by the web server in front of PHP.'}
>
  <form id="site-cache" class="stack" onsubmit={save}>
    <Switch
      label="Compress responses"
      bind:checked={form.values.compression_enabled}
      description="Smaller transfers for text content such as HTML, CSS and JavaScript."
    />
    {#if form.values.compression_enabled}
      <div class="nested stack-sm">
        <Checkbox label="Zstandard" description="Fastest, supported by current browsers." bind:checked={form.values.zstd_enabled} />
        <Checkbox label="Gzip" description="Works everywhere." bind:checked={form.values.gzip_enabled} />
        {#if encoderErr}<p class="small error">{encoderErr}</p>{/if}
      </div>
    {/if}

    <hr class="divider" />

    <Switch
      label="Send a Cache-Control header"
      bind:checked={form.values.cache_control_enabled}
      description="Tells browsers and CDNs how long to keep responses. Leave this off if your application sets its own headers."
    />
    {#if form.values.cache_control_enabled}
      <div class="nested stack-sm">
        <Input
          label="Header value"
          bind:value={form.values.cache_control_value}
          mono
          hint="Supported directives: public, private, no-cache, no-store, must-revalidate, immutable, max-age, s-maxage and similar."
        />
        <div class="row">
          {#each presets as p (p.value)}
            <Button size="sm" variant="ghost" onclick={() => (form.values.cache_control_value = p.value)}>{p.label}</Button>
          {/each}
        </div>
      </div>
    {/if}

    {#if site.site_type !== 'proxy'}
      <hr class="divider" />

      <Switch
        label="Serve cached pages without PHP"
        bind:checked={form.values.page_cache}
        description="For WordPress with WP Super Cache or WP Rocket. The web server follows the plugin's own settings and sends a saved page only where the plugin would send that same page. Every other request goes to WordPress, as it does with this off."
      />
      <div class="nested stack-sm" aria-live="polite">
        {#if pageCache.error}
          <Status kind="error" label="Could not check the cache plugin" />
          <p class="small muted">{pageCache.error}</p>
        {:else if !pageCache.data}
          <Status kind="running" label="Checking the cache plugin…" />
        {:else}
          {@const pc = pageCache.data}
          {#if !pc.available}
            <Status kind="off" label="Needs FastCP's web server" />
            <p class="small muted">The installed web server is not FastCP's build, so cached pages go through PHP. Updating FastCP installs it.</p>
          {:else if !pc.plugin}
            <Status kind="off" label="No supported cache plugin" />
          {:else if pc.serving}
            <Status
              kind={site.page_cache ? 'ok' : 'info'}
              label={site.page_cache ? `Sending pages saved by ${pluginName(pc)}` : `${pluginName(pc)} can be served without PHP`}
            />
          {:else}
            <Status kind="warn" label={`${pluginName(pc)}: pages go to PHP`} />
          {/if}
          {#if pc.reasons?.length}
            {#if pc.serving}<p class="small muted">Some pages go to PHP:</p>{/if}
            <ul class="reasons small">
              {#each pc.reasons as reason, i (i)}<li>{reason}</li>{/each}
            </ul>
          {/if}
        {/if}
        {#if pageCache.data || pageCache.error}
          <div>
            <Button size="sm" variant="ghost" icon={RefreshCw} loading={rechecking} onclick={recheck}>Check again</Button>
          </div>
        {/if}
      </div>
    {/if}
    {#if error}<p class="notice strong" role="alert">{error}</p>{/if}
  </form>
  {#snippet footer()}
    <SaveBar form="site-cache" dirty={form.dirty} saving={form.saving} disabled={!!encoderErr} onreset={() => form.reset()} />
  {/snippet}
</Card>

<style>
  .nested {
    margin-left: 48px;
    max-width: 520px;
  }
  .error {
    font-weight: 500;
  }
  .reasons {
    margin: 0;
    padding-left: 18px;
    color: var(--fg-muted);
  }
  .reasons li + li {
    margin-top: 4px;
  }
  @media (max-width: 520px) {
    .nested {
      margin-left: 0;
    }
  }
</style>
