<script lang="ts">
  import Card from '../../components/Card.svelte';
  import Input from '../../components/Input.svelte';
  import Switch from '../../components/Switch.svelte';
  import Checkbox from '../../components/Checkbox.svelte';
  import Button from '../../components/Button.svelte';
  import SaveBar from '../../components/SaveBar.svelte';
  import { api, errorMessage, seg } from '../../lib/api';
  import { Form } from '../../lib/page.svelte';
  import { toast } from '../../lib/toast.svelte';
  import type { Site } from '../../lib/types';

  let { site, onchange }: { site: Site; onchange: (s: Site) => void } = $props();

  function pick(s: Site) {
    return {
      compression_enabled: s.compression_enabled,
      gzip_enabled: s.gzip_enabled,
      zstd_enabled: s.zstd_enabled,
      cache_control_enabled: s.cache_control_enabled,
      cache_control_value: s.cache_control_value || 'public, max-age=3600',
    };
  }

  // svelte-ignore state_referenced_locally
  const form = new Form(pick(site));
  let error = $state<string | null>(null);

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

<Card title="Caching & compression" description="Applied by the web server in front of PHP.">
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
  @media (max-width: 520px) {
    .nested {
      margin-left: 0;
    }
  }
</style>
