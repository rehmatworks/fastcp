<script lang="ts">
  import Info from '@lucide/svelte/icons/info';
  import Drawer from '../../components/Drawer.svelte';
  import Button from '../../components/Button.svelte';
  import Input from '../../components/Input.svelte';
  import Select from '../../components/Select.svelte';
  import Segmented from '../../components/Segmented.svelte';
  import { api, errorMessage, isAbort } from '../../lib/api';
  import { session } from '../../lib/session.svelte';
  import { router } from '../../lib/router.svelte';
  import { toast } from '../../lib/toast.svelte';
  import { proxyError } from '../../lib/validate';
  import type { Site, SiteType } from '../../lib/types';

  let { open, onclose }: { open: boolean; onclose: () => void } = $props();

  let domain = $state('');
  let siteType = $state<SiteType>('php');
  let php = $state('');
  let upstream = $state('');
  let busy = $state(false);
  let error = $state<string | null>(null);
  let check = $state<{ domain: string; ok: boolean; message: string } | null>(null);

  const versions = $derived(session.meta?.php_versions ?? []);
  const proxy = $derived(siteType === 'proxy');
  const upstreamErr = $derived(proxy && upstream.trim() ? proxyError(upstream) : null);

  $effect(() => {
    if (open) {
      domain = '';
      siteType = 'php';
      php = session.meta?.default_php || versions[0] || '';
      upstream = '';
      error = null;
      check = null;
    }
  });

  // Check availability shortly after the user stops typing.
  $effect(() => {
    const value = domain.trim();
    if (!open || value.length < 3 || !value.includes('.')) {
      check = null;
      return;
    }
    const c = new AbortController();
    const t = setTimeout(async () => {
      try {
        const res = await api.get<{ available: boolean; message?: string; domain?: string }>(
          `/sites/check-domain?domain=${encodeURIComponent(value)}`,
          { signal: c.signal },
        );
        check = { domain: value, ok: res.available, message: res.available ? `${res.domain} is available.` : (res.message ?? 'Not available.') };
      } catch (err) {
        if (!isAbort(err)) check = null;
      }
    }, 350);
    return () => {
      clearTimeout(t);
      c.abort();
    };
  });

  async function submit(e: Event) {
    e.preventDefault();
    busy = true;
    error = null;
    try {
      const body = proxy
        ? { domain: domain.trim(), site_type: siteType, proxy_url: upstream.trim() }
        : { domain: domain.trim(), site_type: siteType, php_version: php };
      const site = await api.post<Site>('/sites', body);
      toast.success(
        siteType === 'wordpress'
          ? `WordPress is ready at ${site.domain}. Open the site to finish setup.`
          : proxy
            ? `${site.domain} now forwards to ${site.proxy_url}.`
            : `${site.domain} is ready.`,
      );
      onclose();
      router.navigate(`/sites/${site.id}`);
    } catch (err) {
      error = errorMessage(err);
    } finally {
      busy = false;
    }
  }
</script>

<Drawer
  {open}
  title="New site"
  description={proxy ? 'Each site gets its own folder and HTTPS certificate.' : 'Each site gets its own folder, PHP pool and HTTPS certificate.'}
  {busy}
  {onclose}
>
  <form id="new-site" class="stack" onsubmit={submit}>
    <Input
      label="Domain"
      bind:value={domain}
      placeholder="example.com"
      autocomplete="off"
      autocapitalize="off"
      spellcheck={false}
      required
      error={check && !check.ok && check.domain === domain.trim() ? check.message : null}
      hint={check?.ok && check.domain === domain.trim() ? check.message : 'You can add more domains after the site is created.'}
    />
    <Segmented
      label="Type"
      showLabel
      bind:value={siteType}
      options={[
        { value: 'php', label: 'PHP app' },
        { value: 'wordpress', label: 'WordPress' },
        { value: 'proxy', label: 'Reverse proxy' },
      ]}
    />
    {#if proxy}
      <Input
        label="Application address"
        bind:value={upstream}
        placeholder="http://127.0.0.1:3000"
        mono
        autocomplete="off"
        autocapitalize="off"
        spellcheck={false}
        required
        error={upstreamErr}
        hint="Where your Node.js, Python, Go or Docker application listens. A port number on its own means 127.0.0.1."
      />
    {:else if versions.length > 0}
      <Select label="PHP version" bind:value={php} options={versions.map((v) => ({ value: v, label: `PHP ${v}` }))} />
    {:else}
      <p class="notice strong">No PHP version is installed yet. {session.isAdmin ? 'Install one under Settings → PHP.' : 'Ask your administrator to install PHP.'}</p>
    {/if}
    {#if siteType === 'wordpress'}
      <p class="notice">
        <Info size={16} strokeWidth={1.75} aria-hidden="true" />
        <span>FastCP downloads WordPress and creates its database. Creating the site can take a minute; afterwards, open the site to choose the admin account.</span>
      </p>
    {:else if proxy}
      <p class="notice">
        <Info size={16} strokeWidth={1.75} aria-hidden="true" />
        <span>The web server handles HTTPS and forwards every request, including WebSockets, to your application. FastCP does not start the application itself.</span>
      </p>
    {/if}
    <p class="muted small">
      Point the domain's DNS A and AAAA records at this server. HTTPS certificates are issued automatically once DNS resolves.
    </p>
    {#if error}<p class="notice strong" role="alert">{error}</p>{/if}
  </form>
  {#snippet footer()}
    <Button onclick={onclose} disabled={busy}>Cancel</Button>
    <Button
      type="submit"
      form="new-site"
      variant="primary"
      loading={busy}
      disabled={!domain.trim() || (proxy ? !upstream.trim() || !!upstreamErr : !php) || (check?.ok === false && check.domain === domain.trim())}
    >
      {busy ? (siteType === 'wordpress' ? 'Installing WordPress…' : 'Creating…') : 'Create site'}
    </Button>
  {/snippet}
</Drawer>
