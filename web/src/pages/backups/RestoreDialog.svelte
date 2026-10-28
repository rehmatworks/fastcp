<script lang="ts">
  import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
  import Dialog from '../../components/Dialog.svelte';
  import Button from '../../components/Button.svelte';
  import Input from '../../components/Input.svelte';
  import { api, errorMessage } from '../../lib/api';
  import { dateTime } from '../../lib/format';
  import type { BackupJob, BackupsResponse, Snapshot } from '../../lib/types';

  interface Props {
    snapshot: Snapshot | null;
    data: BackupsResponse;
    onclose: () => void;
    onstarted: (job: BackupJob) => void;
  }
  let { snapshot, data, onclose, onstarted }: Props = $props();

  interface Choice {
    key: string;
    type: 'site' | 'database';
    id: string;
    name: string;
  }

  const choices = $derived.by((): Choice[] => {
    if (!snapshot) return [];
    const out: Choice[] = [];
    const siteIds = new Set(snapshot.site_ids);
    const siteNames = new Set(snapshot.sites);
    for (const s of data.sites) {
      if (siteIds.has(s.id) || siteNames.has(s.name)) out.push({ key: `s:${s.id}`, type: 'site', id: s.id, name: s.name });
    }
    const dbIds = new Set(snapshot.database_ids);
    const dbNames = new Set(snapshot.databases);
    for (const d of data.databases) {
      if (dbIds.has(d.id) || dbNames.has(d.name)) out.push({ key: `d:${d.id}`, type: 'database', id: d.id, name: d.name });
    }
    return out;
  });

  const missing = $derived.by(() => {
    if (!snapshot) return [];
    const present = new Set(choices.map((c) => c.name));
    return [...snapshot.sites, ...snapshot.databases].filter((n) => !present.has(n));
  });

  let selected = $state('');
  let typed = $state('');
  let busy = $state(false);
  let error = $state<string | null>(null);
  const uid = $props.id();

  $effect(() => {
    if (snapshot) {
      selected = choices[0]?.key ?? '';
      typed = '';
      error = null;
    }
  });

  const choice = $derived(choices.find((c) => c.key === selected));

  async function submit(e: Event) {
    e.preventDefault();
    if (!snapshot || !choice || typed.trim() !== choice.name) return;
    busy = true;
    error = null;
    try {
      const body = choice.type === 'site' ? { type: 'site', site_id: choice.id } : { type: 'database', database_id: choice.id };
      const job = await api.post<BackupJob>(`/backups/snapshots/${encodeURIComponent(snapshot.id)}/restore`, body);
      onstarted(job);
      onclose();
    } catch (err) {
      error = errorMessage(err);
    } finally {
      busy = false;
    }
  }
</script>

<Dialog
  open={snapshot !== null}
  title="Restore from snapshot"
  description={snapshot ? `Taken ${dateTime(snapshot.time)}.` : ''}
  size="lg"
  {busy}
  {onclose}
>
  <form id="restore-{uid}" class="stack" onsubmit={submit}>
    {#if choices.length === 0}
      <p class="notice">Nothing in this snapshot exists on this account any more. Recreate the site or database first, then restore into it.</p>
    {:else}
      <fieldset>
        <legend>What do you want to restore?</legend>
        {#each choices as c (c.key)}
          <label class="choice" class:checked={selected === c.key}>
            <input type="radio" name="restore-item" value={c.key} bind:group={selected} onchange={() => (typed = '')} />
            <span class="text">
              <span class="name">{c.name}</span>
              <span class="muted small">{c.type === 'site' ? 'Site files' : 'Database'}</span>
            </span>
          </label>
        {/each}
      </fieldset>
      {#if missing.length}
        <p class="muted small">Not on this account any more: {missing.join(', ')}.</p>
      {/if}
      {#if choice}
        <p class="notice strong">
          <TriangleAlert size={16} strokeWidth={1.75} aria-hidden="true" />
          <span>
            {#if choice.type === 'site'}
              The files of {choice.name} are replaced with this snapshot. Files added since then are deleted.
            {:else}
              The tables of {choice.name} are replaced with this snapshot. Changes made since then are lost.
            {/if}
          </span>
        </p>
        <Input label="Type {choice.name} to confirm" bind:value={typed} mono autocomplete="off" spellcheck={false} />
      {/if}
    {/if}
    {#if error}<p class="notice strong" role="alert">{error}</p>{/if}
  </form>
  {#snippet footer()}
    <Button onclick={onclose} disabled={busy}>Cancel</Button>
    <Button type="submit" form="restore-{uid}" variant="danger" loading={busy} disabled={!choice || typed.trim() !== choice.name}>
      Restore
    </Button>
  {/snippet}
</Dialog>

<style>
  fieldset {
    border: 0;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  legend {
    font-weight: 500;
    font-size: 13px;
    margin-bottom: 8px;
    padding: 0;
  }
  .choice {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 9px 12px;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    cursor: pointer;
  }
  .choice:hover {
    background: var(--bg-subtle);
  }
  .choice.checked {
    border-color: var(--fg);
    box-shadow: 0 0 0 1px var(--fg);
  }
  input[type='radio'] {
    accent-color: var(--fg);
    margin: 0;
  }
  .text {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .name {
    font-weight: 500;
    overflow-wrap: anywhere;
  }
</style>
