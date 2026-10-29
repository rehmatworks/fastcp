<script lang="ts">
  import Card from '../../components/Card.svelte';
  import Input from '../../components/Input.svelte';
  import SaveBar from '../../components/SaveBar.svelte';
  import { api, errorMessage, seg } from '../../lib/api';
  import { Form } from '../../lib/page.svelte';
  import { toast } from '../../lib/toast.svelte';
  import { proxyError } from '../../lib/validate';
  import type { Site } from '../../lib/types';

  let { site, onchange }: { site: Site; onchange: (s: Site) => void } = $props();

  const pick = (s: Site) => ({ proxy_url: s.proxy_url });

  // svelte-ignore state_referenced_locally
  const form = new Form(pick(site));
  let error = $state<string | null>(null);
  const urlErr = $derived(proxyError(form.values.proxy_url));

  async function save(e: Event) {
    e.preventDefault();
    if (urlErr) return;
    form.saving = true;
    error = null;
    try {
      const next = await api.patch<Site>(`/sites/${seg(site.id)}`, { proxy_url: form.values.proxy_url.trim() });
      form.commit(pick(next));
      onchange(next);
      toast.success(`Requests now go to ${next.proxy_url}.`);
    } catch (err) {
      error = errorMessage(err);
    } finally {
      form.saving = false;
    }
  }
</script>

<Card title="Reverse proxy" description="The web server forwards every request for this site's domains to your application, including WebSocket connections.">
  <form id="site-proxy" class="stack" onsubmit={save}>
    <Input
      label="Application address"
      bind:value={form.values.proxy_url}
      placeholder="http://127.0.0.1:3000"
      mono
      autocomplete="off"
      autocapitalize="off"
      spellcheck={false}
      error={urlErr}
      hint="An http:// or https:// address with a host and a port. A port number on its own means 127.0.0.1."
    />
    <div class="muted small stack-sm">
      <p>
        Let your application listen on 127.0.0.1 so it is reachable only through this site, with HTTPS. It sees the visitor's address in the
        X-Forwarded-For header and the original scheme in X-Forwarded-Proto.
      </p>
      <p>
        FastCP does not start the application. Keep it running with a process manager, or start it with a cron job scheduled
        <span class="mono">@reboot</span>.
      </p>
    </div>
    {#if error}<p class="notice strong" role="alert">{error}</p>{/if}
  </form>
  {#snippet footer()}
    <SaveBar form="site-proxy" dirty={form.dirty} saving={form.saving} disabled={!!urlErr} onreset={() => form.reset()} />
  {/snippet}
</Card>
