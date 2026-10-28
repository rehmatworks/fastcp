<script lang="ts">
  import Card from '../../components/Card.svelte';
  import Input from '../../components/Input.svelte';
  import Switch from '../../components/Switch.svelte';
  import SaveBar from '../../components/SaveBar.svelte';
  import Skeleton from '../../components/Skeleton.svelte';
  import ErrorState from '../../components/ErrorState.svelte';
  import { api, errorMessage } from '../../lib/api';
  import { Form, Resource } from '../../lib/page.svelte';
  import { toast } from '../../lib/toast.svelte';
  import { megabytes } from '../../lib/format';
  import type { MySQLConfig } from '../../lib/types';

  const cfg = new Resource((signal) => api.get<MySQLConfig>('/settings/mysql', { signal }));
  const form = new Form<MySQLConfig>({ buffer_pool_mb: 128, max_connections: 100, perf_schema: false });
  let seeded = false;
  $effect(() => {
    if (cfg.data && !seeded) {
      seeded = true;
      form.reset(pick(cfg.data));
    }
  });
  let error = $state<string | null>(null);

  function pick(c: MySQLConfig): MySQLConfig {
    return { buffer_pool_mb: c.buffer_pool_mb, max_connections: c.max_connections, perf_schema: c.perf_schema };
  }

  const ram = $derived(cfg.data?.detected_ram_mb ?? 0);
  const maxPool = $derived(ram > 0 ? Math.min(65536, Math.floor((ram * 3) / 4)) : 65536);
  const poolErr = $derived.by(() => {
    const n = Number(form.values.buffer_pool_mb);
    if (!Number.isInteger(n) || n < 16) return 'At least 16 MB.';
    if (n > maxPool) return ram > 0 ? `At most ${megabytes(maxPool)} (three quarters of the server's memory).` : 'At most 65536 MB.';
    return null;
  });
  const connErr = $derived.by(() => {
    const n = Number(form.values.max_connections);
    return Number.isInteger(n) && n >= 5 && n <= 5000 ? null : 'Between 5 and 5000.';
  });

  async function save(e: Event) {
    e.preventDefault();
    if (poolErr || connErr) return;
    form.saving = true;
    error = null;
    try {
      const next = await api.put<MySQLConfig>('/settings/mysql', {
        buffer_pool_mb: Number(form.values.buffer_pool_mb),
        max_connections: Number(form.values.max_connections),
        perf_schema: form.values.perf_schema,
      });
      cfg.set(next);
      form.commit(pick(next));
      toast.success('MySQL settings saved and MySQL restarted.');
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
  <Card title="MySQL" description={ram > 0 ? `This server has ${megabytes(ram)} of memory.` : undefined}>
    <form id="mysql" class="stack" onsubmit={save}>
      <div class="grid-2">
        <Input
          label="InnoDB buffer pool"
          type="number"
          bind:value={form.values.buffer_pool_mb}
          suffix="MB"
          min={16}
          max={maxPool}
          error={poolErr}
          hint="The main cache for table data. Around a quarter of the memory suits a server that also runs PHP."
        />
        <Input
          label="Maximum connections"
          type="number"
          bind:value={form.values.max_connections}
          min={5}
          max={5000}
          error={connErr}
          hint="Each connection reserves some memory, so keep this close to what your sites need."
        />
      </div>
      <Switch
        label="Performance schema"
        description="Detailed query instrumentation. Uses a few hundred megabytes of extra memory; leave it off unless you are profiling."
        bind:checked={form.values.perf_schema}
      />
      <p class="muted small">Saving restarts MySQL. Sites lose their database connection for a few seconds.</p>
      {#if error}<p class="notice strong" role="alert">{error}</p>{/if}
    </form>
    {#snippet footer()}
      <SaveBar form="mysql" dirty={form.dirty} saving={form.saving} disabled={!!poolErr || !!connErr} onreset={() => form.reset()} />
    {/snippet}
  </Card>
{/if}
