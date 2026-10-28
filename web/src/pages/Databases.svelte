<script lang="ts">
  import Plus from '@lucide/svelte/icons/plus';
  import DatabaseIcon from '@lucide/svelte/icons/database';
  import ExternalLink from '@lucide/svelte/icons/external-link';
  import KeyRound from '@lucide/svelte/icons/key-round';
  import Trash2 from '@lucide/svelte/icons/trash-2';
  import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
  import PageHeader from '../components/PageHeader.svelte';
  import Button from '../components/Button.svelte';
  import Skeleton from '../components/Skeleton.svelte';
  import ErrorState from '../components/ErrorState.svelte';
  import EmptyState from '../components/EmptyState.svelte';
  import Drawer from '../components/Drawer.svelte';
  import Dialog from '../components/Dialog.svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import Input from '../components/Input.svelte';
  import CopyField from '../components/CopyField.svelte';
  import Menu from '../components/Menu.svelte';
  import { api, errorMessage, seg } from '../lib/api';
  import { Resource } from '../lib/page.svelte';
  import { session } from '../lib/session.svelte';
  import { toast } from '../lib/toast.svelte';
  import { date } from '../lib/format';
  import type { Database, DatabaseCredentials } from '../lib/types';

  const dbs = new Resource((signal) => api.get<Database[]>('/databases', { signal }));

  let creating = $state(false);
  let name = $state('');
  let createBusy = $state(false);
  let createError = $state<string | null>(null);
  let credentials = $state<{ creds: DatabaseCredentials; title: string } | null>(null);
  let resetting = $state<Database | null>(null);
  let deleting = $state<Database | null>(null);
  let opening = $state<string | null>(null);

  const prefix = $derived(`${(session.user?.username ?? '').replaceAll('-', '_')}_`);
  const nameErr = $derived(name && !/^[a-z][a-z0-9_]{0,39}$/.test(name) ? 'Start with a letter; use lowercase letters, digits and _ (40 characters at most).' : null);

  async function create(e: Event) {
    e.preventDefault();
    createBusy = true;
    createError = null;
    try {
      const creds = await api.post<DatabaseCredentials>('/databases', { name });
      creating = false;
      credentials = { creds, title: 'Database created' };
      await dbs.reload({ quiet: true });
    } catch (err) {
      createError = errorMessage(err);
    } finally {
      createBusy = false;
    }
  }

  async function openPMA(d: Database) {
    // Open the tab synchronously so pop-up blockers allow it, then point it
    // at the single-use link once the server has issued one.
    const win = window.open('about:blank', '_blank');
    opening = d.id;
    try {
      const { url } = await api.post<{ url: string }>(`/databases/${seg(d.id)}/phpmyadmin`);
      if (win) {
        win.opener = null;
        win.location.href = url;
      } else {
        location.href = url;
      }
    } catch (err) {
      win?.close();
      toast.error(errorMessage(err));
    } finally {
      opening = null;
    }
  }
</script>

<PageHeader title="Databases" description="MySQL databases for your sites. Each database has its own user.">
  {#snippet actions()}
    <Button
      variant="primary"
      icon={Plus}
      onclick={() => {
        name = '';
        createError = null;
        creating = true;
      }}>New database</Button
    >
  {/snippet}
</PageHeader>

{#if dbs.loading}
  <Skeleton rows={3} />
{:else if dbs.error}
  <ErrorState message={dbs.error} onretry={() => dbs.reload()} />
{:else if (dbs.data?.length ?? 0) === 0}
  <EmptyState icon={DatabaseIcon} title="No databases yet" description="WordPress sites get a database automatically. Create one here for other applications.">
    <Button variant="primary" icon={Plus} onclick={() => (creating = true)}>New database</Button>
  </EmptyState>
{:else}
  <div class="stack">
    <div class="table-wrap">
      <table class="table">
        <thead>
          <tr>
            <th>Database</th>
            <th>User</th>
            <th>Created</th>
            <th class="actions"><span class="sr-only">Actions</span></th>
          </tr>
        </thead>
        <tbody>
          {#each dbs.data ?? [] as d (d.id)}
            <tr>
              <td class="primary-cell"><span class="mono cell-title">{d.db_name}</span></td>
              <td data-label="User" class="mono">{d.db_user}</td>
              <td data-label="Created" class="muted nowrap">{date(d.created_at)}</td>
              <td class="actions">
                <div class="row nowrap">
                  <Button size="sm" icon={ExternalLink} loading={opening === d.id} onclick={() => openPMA(d)}>phpMyAdmin</Button>
                  <Menu
                    label="Actions for {d.db_name}"
                    items={[
                      { label: 'Reset password', icon: KeyRound, onclick: () => (resetting = d) },
                      { label: 'Delete', icon: Trash2, destructive: true, onclick: () => (deleting = d) },
                    ]}
                  />
                </div>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
    <p class="muted small">Connect from your sites with host <span class="mono">localhost</span> and port <span class="mono">3306</span>.</p>
  </div>
{/if}

<Drawer open={creating} title="New database" description="A MySQL user with access to only this database is created with it." busy={createBusy} onclose={() => (creating = false)}>
  <form id="new-db" class="stack" onsubmit={create}>
    <Input
      label="Name"
      bind:value={name}
      {prefix}
      mono
      autocomplete="off"
      autocapitalize="off"
      spellcheck={false}
      required
      error={nameErr ?? createError}
      hint="The account name is added in front so names never collide with other users."
    />
  </form>
  {#snippet footer()}
    <Button onclick={() => (creating = false)} disabled={createBusy}>Cancel</Button>
    <Button type="submit" form="new-db" variant="primary" loading={createBusy} disabled={!name || !!nameErr}>Create database</Button>
  {/snippet}
</Drawer>

<Dialog open={credentials !== null} title={credentials?.title ?? ''} onclose={() => (credentials = null)}>
  {#if credentials}
    {@const c = credentials.creds}
    <div class="stack">
      <p class="notice strong">
        <TriangleAlert size={16} strokeWidth={1.75} aria-hidden="true" />
        <span>Copy the password now. It is not shown again, but you can reset it at any time.</span>
      </p>
      <CopyField label="Database" value={c.db_name} />
      <CopyField label="User" value={c.db_user} />
      <CopyField label="Password" value={c.password} secret />
      <CopyField label="Host" value="{c.host}:{c.port}" />
    </div>
  {/if}
  {#snippet footer()}
    <Button variant="primary" onclick={() => (credentials = null)}>Done</Button>
  {/snippet}
</Dialog>

<ConfirmDialog
  open={resetting !== null}
  title="Reset the password for {resetting?.db_user}?"
  message="Applications using the current password lose access until you update their configuration."
  actionLabel="Reset password"
  destructive={false}
  onclose={() => (resetting = null)}
  onconfirm={async () => {
    if (!resetting) return;
    const creds = await api.post<DatabaseCredentials>(`/databases/${seg(resetting.id)}/reset-password`);
    credentials = { creds, title: 'New password' };
  }}
/>

<ConfirmDialog
  open={deleting !== null}
  title="Delete {deleting?.db_name}?"
  message="The database, its tables and its user are deleted permanently. Sites that use it stop working."
  confirmText={deleting?.db_name}
  actionLabel="Delete database"
  onclose={() => (deleting = null)}
  onconfirm={async () => {
    if (!deleting) return;
    const name = deleting.db_name;
    await api.del(`/databases/${seg(deleting.id)}`);
    toast.success(`${name} was deleted.`);
    await dbs.reload({ quiet: true });
  }}
/>
