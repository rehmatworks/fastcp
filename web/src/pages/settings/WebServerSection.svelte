<script lang="ts">
  import Card from '../../components/Card.svelte';
  import Input from '../../components/Input.svelte';
  import Switch from '../../components/Switch.svelte';
  import Segmented from '../../components/Segmented.svelte';
  import SaveBar from '../../components/SaveBar.svelte';
  import Skeleton from '../../components/Skeleton.svelte';
  import ErrorState from '../../components/ErrorState.svelte';
  import CustomCaddyCard from './CustomCaddyCard.svelte';
  import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
  import { api, errorMessage } from '../../lib/api';
  import { Form, Resource } from '../../lib/page.svelte';
  import { toast } from '../../lib/toast.svelte';
  import type { CaddyConfig, CaddyProfile } from '../../lib/types';

  const cfg = new Resource((signal) => api.get<CaddyConfig>('/settings/webserver', { signal }));
  const form = new Form<CaddyConfig>({
    profile: 'low_ram',
    access_logs: false,
    read_header: '',
    read_body: '',
    write_timeout: '',
    idle_timeout: '',
    grace_period: '',
    max_header_size: 0,
  });
  let seeded = false;
  $effect(() => {
    if (cfg.data && !seeded) {
      seeded = true;
      form.reset(cfg.data);
    }
  });
  let error = $state<string | null>(null);

  const profiles: { value: CaddyProfile; label: string }[] = [
    { value: 'low_ram', label: 'Low memory' },
    { value: 'balanced', label: 'Balanced' },
    { value: 'high_throughput', label: 'High traffic' },
    { value: 'custom', label: 'Custom' },
  ];
  const descriptions: Record<CaddyProfile, string> = {
    low_ram: 'Short timeouts and small buffers. Best for servers with 1–2 GB of memory.',
    balanced: 'Suits most servers. Timeouts scale with the available memory.',
    high_throughput: 'Long keep-alives and large headers for busy sites and APIs.',
    custom: 'Set every timeout yourself.',
  };

  const v = $derived(form.values);
  const custom = $derived(v.profile === 'custom');
  const profileChanged = $derived(!custom && v.profile !== form.saved.profile);

  const durationRe = /^\d+(\.\d+)?(ms|s|m)$/;
  function durErr(s: string): string | null {
    return !custom || durationRe.test(s.trim()) ? null : 'Use a duration such as 30s or 2m.';
  }
  const errors = $derived({
    read_header: durErr(v.read_header),
    read_body: durErr(v.read_body),
    write_timeout: durErr(v.write_timeout),
    idle_timeout: durErr(v.idle_timeout),
    grace_period: durErr(v.grace_period),
    max_header_size:
      custom && (Number(v.max_header_size) < 4096 || Number(v.max_header_size) > 262144) ? 'Between 4096 and 262144 bytes.' : null,
  });
  const invalid = $derived(Object.values(errors).some(Boolean));

  async function save(e: Event) {
    e.preventDefault();
    if (invalid) return;
    form.saving = true;
    error = null;
    try {
      const next = await api.put<CaddyConfig>('/settings/webserver', { ...form.values, max_header_size: Number(form.values.max_header_size) });
      cfg.set(next);
      form.commit(next);
      toast.success('Web server settings saved. Caddy reloaded without dropping connections.');
    } catch (err) {
      error = errorMessage(err);
    } finally {
      form.saving = false;
    }
  }
</script>

{#if cfg.loading}
  <Skeleton variant="card" rows={2} />
{:else if cfg.error || !cfg.data}
  <ErrorState message={cfg.error ?? 'Unknown error'} onretry={() => cfg.reload()} />
{:else}
  <Card title="Caddy" description="Caddy serves every site and manages HTTPS certificates.">
    <form id="webserver" class="stack" onsubmit={save}>
      <div class="stack-sm">
        <Segmented label="Performance profile" showLabel bind:value={form.values.profile} options={profiles} />
        <p class="muted small">{descriptions[v.profile]}</p>
      </div>

      {#if profileChanged}
        <p class="notice">The timeouts for this profile are applied when you save.</p>
      {:else}
        <div class="grid-3">
          <Input label="Header read timeout" bind:value={form.values.read_header} mono disabled={!custom} error={errors.read_header} />
          <Input label="Body read timeout" bind:value={form.values.read_body} mono disabled={!custom} error={errors.read_body} />
          <Input label="Write timeout" bind:value={form.values.write_timeout} mono disabled={!custom} error={errors.write_timeout} />
          <Input label="Idle timeout" bind:value={form.values.idle_timeout} mono disabled={!custom} error={errors.idle_timeout} />
          <Input label="Shutdown grace period" bind:value={form.values.grace_period} mono disabled={!custom} error={errors.grace_period} />
          <Input
            label="Maximum header size"
            type="number"
            bind:value={form.values.max_header_size}
            suffix="bytes"
            disabled={!custom}
            min={4096}
            max={262144}
            error={errors.max_header_size}
          />
        </div>
        {#if custom}<p class="muted small">Durations between 1s and 10m.</p>{/if}
      {/if}

      <div class="divider"></div>
      <Switch
        label="Access logs"
        description="Record every request to every site. You can read them in each site's Logs tab."
        bind:checked={form.values.access_logs}
      />
      {#if v.access_logs && !form.saved.access_logs}
        <p class="notice strong">
          <TriangleAlert size={16} strokeWidth={1.75} aria-hidden="true" />
          <span
            >Every request is written to disk, which slows the web server down on busy sites and can use a lot of storage. Each site keeps up to
            50 MB of current log plus 5 older compressed files. Turn access logs off again when you are done debugging.</span
          >
        </p>
      {/if}
      {#if error}<p class="notice strong" role="alert">{error}</p>{/if}
    </form>
    {#snippet footer()}
      <SaveBar form="webserver" dirty={form.dirty} saving={form.saving} disabled={invalid} onreset={() => form.reset()} />
    {/snippet}
  </Card>
  <div class="gap"><CustomCaddyCard /></div>
{/if}

<style>
  .gap {
    margin-top: 24px;
  }
</style>
