<script lang="ts">
  import RefreshCw from '@lucide/svelte/icons/refresh-cw';
  import ScrollText from '@lucide/svelte/icons/scroll-text';
  import Card from '../../components/Card.svelte';
  import Button from '../../components/Button.svelte';
  import Segmented from '../../components/Segmented.svelte';
  import Switch from '../../components/Switch.svelte';
  import Skeleton from '../../components/Skeleton.svelte';
  import ErrorState from '../../components/ErrorState.svelte';
  import EmptyState from '../../components/EmptyState.svelte';
  import Status from '../../components/Status.svelte';
  import { api, seg } from '../../lib/api';
  import { Resource } from '../../lib/page.svelte';
  import { session } from '../../lib/session.svelte';
  import { bytes, dateTime, relative } from '../../lib/format';
  import type { AccessEntry, Site, SiteLog, SiteLogKind } from '../../lib/types';

  let { site }: { site: Site } = $props();

  let kind = $state<SiteLogKind>('php');
  let errorsOnly = $state(false);
  let refreshing = $state(false);
  let logEl = $state<HTMLPreElement | null>(null);

  const log = new Resource((signal) => api.get<SiteLog>(`/sites/${seg(site.id)}/logs/${kind}`, { signal }));

  // The previous log stays in data while the other one loads.
  const current = $derived(log.data?.kind === kind ? log.data : undefined);
  const entries = $derived((current?.entries ?? []).filter((e) => !errorsOnly || e.status >= 400));
  const count = $derived(kind === 'php' ? (current?.lines.length ?? 0) : (current?.entries.length ?? 0));

  $effect(() => {
    if (logEl && current?.lines.length) logEl.scrollTop = logEl.scrollHeight;
  });

  async function refresh() {
    refreshing = true;
    await log.reload({ quiet: true });
    refreshing = false;
  }

  function statusKind(e: AccessEntry) {
    return e.status >= 500 ? 'error' : e.status >= 400 ? 'warn' : 'ok';
  }
</script>

<Card title="Logs" description="The most recent entries for this site. Older entries stay in the log files on the server.">
  {#snippet actions()}
    <Button size="sm" icon={RefreshCw} loading={refreshing} onclick={refresh}>Refresh</Button>
  {/snippet}
  <div class="stack">
    <div class="toolbar">
      <Segmented
        label="Log"
        bind:value={kind}
        options={[
          { value: 'php', label: 'PHP errors' },
          { value: 'access', label: 'Access log' },
        ]}
        onchange={() => log.reload()}
      />
      {#if kind === 'access' && current?.entries.length}
        <Switch label="Errors only" bind:checked={errorsOnly} />
      {/if}
    </div>

    {#if log.error}
      <ErrorState message={log.error} onretry={() => log.reload()} />
    {:else if !current}
      <Skeleton rows={4} />
    {:else if kind === 'access' && !current.enabled}
      <EmptyState
        icon={ScrollText}
        title="Access logs are turned off"
        description={session.isAdmin
          ? 'Turn on access logs in the web server settings to record every request to the sites on this server.'
          : 'Access logs are turned off on this server. Your administrator can turn them on.'}
        compact
      >
        {#if session.isAdmin}<Button href="/settings/webserver">Web server settings</Button>{/if}
      </EmptyState>
    {:else if count === 0}
      <EmptyState
        icon={ScrollText}
        title={kind === 'php' ? 'No PHP errors logged' : 'No requests logged yet'}
        description={kind === 'php'
          ? 'Errors, warnings and notices from PHP appear here when the site produces them.'
          : 'Requests appear here as visitors open the site.'}
        compact
      />
    {:else if kind === 'php'}
      <pre class="log tall" bind:this={logEl}>{current.lines.join('\n')}</pre>
    {:else if entries.length === 0}
      <EmptyState icon={ScrollText} title="No errors" description="None of the recent requests returned an error." compact />
    {:else}
      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr>
              <th>Request</th>
              <th>Status</th>
              <th>Time</th>
              <th>Client</th>
            </tr>
          </thead>
          <tbody>
            {#each entries as e, i (i)}
              <tr>
                <td class="primary-cell">
                  <div class="cell-title mono request">{e.method} {e.uri}</div>
                  <div class="cell-sub">{e.host} · {bytes(e.size)} in {e.duration_ms} ms</div>
                </td>
                <td data-label="Status"><Status kind={statusKind(e)} label={String(e.status)} /></td>
                <td data-label="Time" class="nowrap" title={dateTime(e.time)}>{relative(e.time)}</td>
                <td data-label="Client">
                  <div class="mono">{e.ip}</div>
                  {#if e.user_agent}<div class="cell-sub agent" title={e.user_agent}>{e.user_agent}</div>{/if}
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}

    {#if current && (current.exists || kind === 'php')}
      <p class="muted small meta">
        <span class="mono">{current.path}</span>
        {#if current.exists}
          · {bytes(current.size)} · updated {relative(current.updated_at)}
          {#if current.truncated}· showing the last {count} {kind === 'php' ? 'lines' : 'requests'}{/if}
        {/if}
      </p>
    {/if}
  </div>
</Card>

<style>
  .toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
  }
  .tall {
    max-height: 520px;
    margin: 0;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .request {
    font-size: 13px;
    overflow-wrap: anywhere;
  }
  .agent {
    max-width: 220px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .nowrap {
    white-space: nowrap;
  }
  .meta {
    overflow-wrap: anywhere;
  }
</style>
