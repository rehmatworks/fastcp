<script lang="ts">
  import RefreshCw from '@lucide/svelte/icons/refresh-cw';
  import Card from '../../components/Card.svelte';
  import Button from '../../components/Button.svelte';
  import Status from '../../components/Status.svelte';
  import Skeleton from '../../components/Skeleton.svelte';
  import ErrorState from '../../components/ErrorState.svelte';
  import { api, errorMessage } from '../../lib/api';
  import { Resource } from '../../lib/page.svelte';
  import { toast } from '../../lib/toast.svelte';
  import type { StatusKind } from '../../lib/status';
  import type { CustomCaddyFile, CustomCaddyStatus } from '../../lib/types';

  const custom = new Resource((signal) => api.get<CustomCaddyStatus>('/settings/webserver/custom', { signal }));
  let applying = $state(false);
  let error = $state<string | null>(null);

  const states: Record<CustomCaddyFile['state'], { kind: StatusKind; label: string }> = {
    applied: { kind: 'ok', label: 'Applied' },
    pending: { kind: 'pending', label: 'Not applied yet' },
    removed: { kind: 'warn', label: 'Deleted, still loaded' },
    ignored: { kind: 'error', label: 'Ignored' },
  };

  async function apply() {
    applying = true;
    error = null;
    try {
      custom.set(await api.post<CustomCaddyStatus>('/settings/webserver/custom/apply'));
      toast.success('Custom configuration applied. Caddy reloaded without dropping connections.');
    } catch (err) {
      error = errorMessage(err);
      void custom.reload({ quiet: true });
    } finally {
      applying = false;
    }
  }
</script>

<Card
  title="Custom configuration"
  description="FastCP writes the Caddyfile itself and overwrites manual edits. Put your own Caddy configuration in these files instead; FastCP never changes them."
>
  {#if custom.loading}
    <Skeleton rows={2} />
  {:else if custom.error || !custom.data}
    <ErrorState message={custom.error ?? 'Unknown error'} onretry={() => custom.reload()} />
  {:else}
    {@const st = custom.data}
    <div class="stack">
      <ul class="paths">
        <li>
          <span class="mono">{st.dir}/*.caddy</span>
          <span class="muted small">Imported at the top level, for your own site blocks and snippets.</span>
        </li>
        <li>
          <span class="mono">{st.dir}/sites/&lt;user&gt;/&lt;site&gt;.caddy</span>
          <span class="muted small">Imported inside one site's block. Each site's overview shows its file.</span>
        </li>
      </ul>
      <p class="muted small">
        Edit the files as root over SSH. They must be owned by root and not writable by group or others. Caddy checks the whole configuration
        before applying it, and keeps the running configuration if anything is wrong.
      </p>
      {#if st.files.length}
        <div class="table-wrap">
          <table class="table">
            <thead>
              <tr>
                <th>File</th>
                <th>Site</th>
                <th>Status</th>
              </tr>
            </thead>
            <tbody>
              {#each st.files as f (f.path)}
                {@const s = states[f.state]}
                <tr>
                  <td class="primary-cell">
                    <span class="mono cell-title">{f.path.replace(`${st.dir}/`, '')}</span>
                    {#if f.problem}<div class="cell-sub">{f.problem}</div>{/if}
                  </td>
                  <td data-label="Site" class="nowrap">
                    {#if f.domain || f.username}
                      {f.domain || `${f.username}/${f.slug}`}
                    {:else if f.path.startsWith(`${st.dir}/sites/`)}
                      <span class="muted">—</span>
                    {:else}
                      Top level
                    {/if}
                  </td>
                  <td data-label="Status"><Status kind={s.kind} label={s.label} /></td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {:else}
        <p class="muted">No custom files yet.</p>
      {/if}
      {#if error}<p class="notice strong pre" role="alert">{error}</p>{/if}
    </div>
  {/if}
  {#snippet footer()}
    <div class="row">
      <Button icon={RefreshCw} onclick={() => custom.reload({ quiet: true })} disabled={applying}>Check again</Button>
      <Button variant="primary" loading={applying} disabled={!custom.data?.pending} onclick={apply}>Apply</Button>
    </div>
  {/snippet}
</Card>

<style>
  .paths {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 12px;
  }
  .paths li {
    display: grid;
    gap: 2px;
    overflow-wrap: anywhere;
  }
  .pre {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .row {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    width: 100%;
  }
</style>
