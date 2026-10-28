<script lang="ts">
  import Card from '../../components/Card.svelte';
  import Input from '../../components/Input.svelte';
  import Select from '../../components/Select.svelte';
  import SaveBar from '../../components/SaveBar.svelte';
  import { api, errorMessage, seg } from '../../lib/api';
  import { Form } from '../../lib/page.svelte';
  import { session } from '../../lib/session.svelte';
  import { toast } from '../../lib/toast.svelte';
  import { parseSize } from '../../lib/validate';
  import type { Site } from '../../lib/types';

  let { site, onchange }: { site: Site; onchange: (s: Site) => void } = $props();

  function pick(s: Site) {
    return {
      php_version: s.php_version,
      php_memory_limit: s.php_memory_limit,
      php_post_max_size: s.php_post_max_size,
      php_upload_max_filesize: s.php_upload_max_filesize,
      php_max_execution_time: s.php_max_execution_time,
      php_max_input_vars: s.php_max_input_vars,
    };
  }

  // svelte-ignore state_referenced_locally
  const form = new Form(pick(site));
  let error = $state<string | null>(null);

  const versions = $derived.by(() => {
    const list = [...(session.meta?.php_versions ?? [])];
    if (!list.includes(site.php_version)) list.push(site.php_version);
    return list.sort().map((v) => ({
      value: v,
      label: session.meta?.php_versions.includes(v) ? `PHP ${v}` : `PHP ${v} (not installed)`,
    }));
  });

  const v = $derived(form.values);
  const sizeErr = (s: string, min: number) => {
    const n = parseSize(s);
    if (n === null) return 'Use a number with K, M or G, for example 256M.';
    if (n < min) return `Must be at least ${min / (1 << 20)}M.`;
    return null;
  };
  const memErr = $derived(sizeErr(v.php_memory_limit, 16 << 20));
  const postErr = $derived(sizeErr(v.php_post_max_size, 1 << 20));
  const uploadErr = $derived.by(() => {
    const e = sizeErr(v.php_upload_max_filesize, 1 << 20);
    if (e) return e;
    const up = parseSize(v.php_upload_max_filesize) ?? 0;
    const post = parseSize(v.php_post_max_size) ?? 0;
    return post && up > post ? 'Cannot be larger than the maximum POST size.' : null;
  });
  const timeErr = $derived(v.php_max_execution_time < 1 || v.php_max_execution_time > 3600 ? 'Between 1 and 3600 seconds.' : null);
  const varsErr = $derived(v.php_max_input_vars < 100 || v.php_max_input_vars > 100000 ? 'Between 100 and 100000.' : null);
  const invalid = $derived(!!(memErr || postErr || uploadErr || timeErr || varsErr));

  async function save(e: Event) {
    e.preventDefault();
    if (invalid) return;
    form.saving = true;
    error = null;
    try {
      const next = await api.patch<Site>(`/sites/${seg(site.id)}`, {
        ...form.values,
        php_max_execution_time: Number(form.values.php_max_execution_time),
        php_max_input_vars: Number(form.values.php_max_input_vars),
      });
      form.commit(pick(next));
      onchange(next);
      toast.success('PHP settings saved. The site’s PHP pool was reloaded.');
    } catch (err) {
      error = errorMessage(err);
    } finally {
      form.saving = false;
    }
  }
</script>

<Card title="PHP" description="These settings apply to this site only.">
  <form id="site-php" class="stack" onsubmit={save}>
    <div class="grid-2">
      <Select
        label="PHP version"
        bind:value={form.values.php_version}
        options={versions}
        hint="Switching versions restarts the site’s PHP pool."
      />
      <Input label="Memory limit" bind:value={form.values.php_memory_limit} mono error={memErr} hint="memory_limit, for example 256M." />
      <Input label="Maximum POST size" bind:value={form.values.php_post_max_size} mono error={postErr} hint="post_max_size" />
      <Input label="Maximum upload size" bind:value={form.values.php_upload_max_filesize} mono error={uploadErr} hint="upload_max_filesize" />
      <Input
        label="Maximum execution time"
        type="number"
        bind:value={form.values.php_max_execution_time}
        suffix="seconds"
        min={1}
        max={3600}
        error={timeErr}
      />
      <Input label="Maximum input variables" type="number" bind:value={form.values.php_max_input_vars} min={100} max={100000} error={varsErr} />
    </div>
    {#if error}<p class="notice strong" role="alert">{error}</p>{/if}
  </form>
  {#snippet footer()}
    <SaveBar form="site-php" dirty={form.dirty} saving={form.saving} disabled={invalid} onreset={() => form.reset()} />
  {/snippet}
</Card>
