<script lang="ts">
  import Cloud from '@lucide/svelte/icons/cloud';
  import Plus from '@lucide/svelte/icons/plus';
  import RefreshCw from '@lucide/svelte/icons/refresh-cw';
  import Star from '@lucide/svelte/icons/star';
  import Trash2 from '@lucide/svelte/icons/trash-2';
  import Card from '../../components/Card.svelte';
  import Button from '../../components/Button.svelte';
  import Input from '../../components/Input.svelte';
  import Switch from '../../components/Switch.svelte';
  import Status from '../../components/Status.svelte';
  import Badge from '../../components/Badge.svelte';
  import Menu from '../../components/Menu.svelte';
  import ConfirmDialog from '../../components/ConfirmDialog.svelte';
  import { api, errorMessage, seg } from '../../lib/api';
  import { toast } from '../../lib/toast.svelte';
  import { sslView } from '../../lib/status';
  import { date, relative } from '../../lib/format';
  import type { Domain, Site } from '../../lib/types';

  let { site, onchange }: { site: Site; onchange: (s: Site) => void } = $props();

  let newDomain = $state('');
  let adding = $state(false);
  let addError = $state<string | null>(null);
  let checking = $state(false);
  let savingHTTPS = $state(false);
  let removing = $state<Domain | null>(null);
  let busyDomain = $state<number | null>(null);

  const base = $derived(`/sites/${seg(site.id)}`);
  const domains = $derived([...site.domains].sort((a, b) => Number(b.is_primary) - Number(a.is_primary) || a.domain.localeCompare(b.domain)));
  const allValid = $derived(site.domains.every((d) => site.ssl[d.domain]?.valid));
  const behindCloudflare = $derived(site.domains.some((d) => site.ssl[d.domain]?.proxy === 'cloudflare'));

  async function add(e: Event) {
    e.preventDefault();
    adding = true;
    addError = null;
    try {
      onchange(await api.post<Site>(`${base}/domains`, { domain: newDomain.trim(), redirect_to_primary: false }));
      toast.success(`${newDomain.trim()} was added.`);
      newDomain = '';
    } catch (err) {
      addError = errorMessage(err);
    } finally {
      adding = false;
    }
  }

  async function run(d: Domain, fn: () => Promise<Site>, done?: string) {
    busyDomain = d.id;
    try {
      onchange(await fn());
      if (done) toast.success(done);
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      busyDomain = null;
    }
  }

  async function recheck() {
    checking = true;
    try {
      const next = await api.post<Site>(`${base}/ssl-check`);
      onchange(next);
      const ok = next.domains.every((d) => next.ssl[d.domain]?.valid);
      toast.info(ok ? 'All certificates are valid.' : 'Some domains do not have a valid certificate yet.');
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      checking = false;
    }
  }

  async function setForceHTTPS(on: boolean) {
    savingHTTPS = true;
    try {
      onchange(await api.patch<Site>(base, { force_https: on }));
      toast.success(on ? 'Visitors are now redirected to HTTPS.' : 'The HTTPS redirect is off.');
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      savingHTTPS = false;
    }
  }
</script>

<div class="stack">
  <Card title="Domains" description="Every domain serves this site. Certificates are issued automatically once DNS points here.">
    {#snippet actions()}
      <Button size="sm" icon={RefreshCw} loading={checking} onclick={recheck}>Check HTTPS</Button>
    {/snippet}
    <div class="stack">
      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr>
              <th>Domain</th>
              <th>HTTPS</th>
              <th>Redirect to primary</th>
              <th class="actions"><span class="sr-only">Actions</span></th>
            </tr>
          </thead>
          <tbody>
            {#each domains as d (d.id)}
              {@const st = site.ssl[d.domain]}
              {@const v = sslView(st)}
              <tr>
                <td class="primary-cell">
                  <div class="row">
                    <span class="cell-title domain">{d.domain}</span>
                    {#if d.is_primary}<Badge strong>Primary</Badge>{/if}
                    {#if st?.proxy === 'cloudflare'}<Badge>Cloudflare</Badge>{/if}
                  </div>
                  {#if st && !st.valid && st.message}
                    <div class="cell-sub">{st.message}</div>
                  {:else if st?.valid && st.expires_at}
                    <div class="cell-sub">{st.issuer ? `${st.issuer} · ` : ''}valid until {date(st.expires_at)}</div>
                  {/if}
                </td>
                <td data-label="HTTPS">
                  <Status kind={v.kind} label={v.label} title={st ? `Checked ${relative(st.checked_at)}` : undefined} />
                </td>
                <td data-label="Redirect to primary">
                  {#if d.is_primary}
                    <span class="muted small">—</span>
                  {:else}
                    <Switch
                      label="Redirect {d.domain} to {site.domain}"
                      hideLabel
                      checked={d.redirect_to_primary}
                      disabled={busyDomain === d.id}
                      onchange={(on) =>
                        run(d, () => api.patch<Site>(`${base}/domains/${d.id}`, { redirect_to_primary: on }))}
                    />
                  {/if}
                </td>
                <td class="actions">
                  {#if !d.is_primary}
                    <Menu
                      label="Actions for {d.domain}"
                      items={[
                        {
                          label: 'Make primary',
                          icon: Star,
                          onclick: () => run(d, () => api.post<Site>(`${base}/domains/${d.id}/primary`), `${d.domain} is now the primary domain.`),
                        },
                        { label: 'Remove', icon: Trash2, destructive: true, onclick: () => (removing = d) },
                      ]}
                    />
                  {/if}
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>

      <form class="add" onsubmit={add}>
        <Input
          label="Add a domain"
          bind:value={newDomain}
          placeholder="www.example.com"
          autocomplete="off"
          autocapitalize="off"
          spellcheck={false}
          error={addError}
        />
        <Button type="submit" icon={Plus} loading={adding} disabled={!newDomain.trim()}>Add</Button>
      </form>
    </div>
  </Card>

  <Card title="HTTPS redirect">
    <div class="stack">
      <Switch
        label="Redirect HTTP to HTTPS"
        checked={site.force_https}
        disabled={savingHTTPS}
        onchange={setForceHTTPS}
        description={site.force_https
          ? 'Visitors who open the site over HTTP are sent to HTTPS.'
          : behindCloudflare
            ? 'Cloudflare can redirect visitors itself (Always Use HTTPS), so FastCP does not turn this on automatically.'
            : allValid
              ? 'Every domain has a valid certificate, so this is safe to turn on.'
              : 'FastCP turns this on automatically once every domain has a valid certificate.'}
      />
      {#if behindCloudflare}
        <p class="notice">
          <Cloud size={16} strokeWidth={1.75} aria-hidden="true" />
          <span
            >This site is behind Cloudflare's proxy. In Cloudflare, set SSL/TLS encryption to <strong>Full (strict)</strong>. With
            Flexible, Cloudflare reaches this server over plain HTTP, and the site cannot load while the redirect is on.</span
          >
        </p>
      {/if}
    </div>
  </Card>
</div>

<ConfirmDialog
  open={removing !== null}
  title="Remove {removing?.domain}?"
  message="The domain stops serving this site immediately. Its certificate is kept by the web server until it expires."
  actionLabel="Remove domain"
  onclose={() => (removing = null)}
  onconfirm={async () => {
    if (!removing) return;
    const d = removing;
    onchange(await api.del<Site>(`${base}/domains/${d.id}`));
    toast.success(`${d.domain} was removed.`);
  }}
/>

<style>
  .domain {
    overflow-wrap: anywhere;
  }
  .add {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    max-width: 520px;
  }
  .add :global(.field) {
    flex: 1;
  }
  .add :global(.btn) {
    margin-top: 25px;
  }
</style>
