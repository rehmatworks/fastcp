<script lang="ts">
  import Plus from '@lucide/svelte/icons/plus';
  import UsersIcon from '@lucide/svelte/icons/users';
  import SearchX from '@lucide/svelte/icons/search-x';
  import Pencil from '@lucide/svelte/icons/pencil';
  import LogIn from '@lucide/svelte/icons/log-in';
  import KeyRound from '@lucide/svelte/icons/key-round';
  import Ban from '@lucide/svelte/icons/ban';
  import CircleCheck from '@lucide/svelte/icons/circle-check';
  import Trash2 from '@lucide/svelte/icons/trash-2';
  import PageHeader from '../components/PageHeader.svelte';
  import Button from '../components/Button.svelte';
  import SearchInput from '../components/SearchInput.svelte';
  import Skeleton from '../components/Skeleton.svelte';
  import ErrorState from '../components/ErrorState.svelte';
  import EmptyState from '../components/EmptyState.svelte';
  import Badge from '../components/Badge.svelte';
  import Menu from '../components/Menu.svelte';
  import Pagination from '../components/Pagination.svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import UserDrawer from './users/UserDrawer.svelte';
  import ImpersonateDialog from './users/ImpersonateDialog.svelte';
  import SetPasswordDialog from './users/SetPasswordDialog.svelte';
  import { api, seg } from '../lib/api';
  import { Resource } from '../lib/page.svelte';
  import { router } from '../lib/router.svelte';
  import { session } from '../lib/session.svelte';
  import { toast } from '../lib/toast.svelte';
  import { date, limit, megabytes, plural } from '../lib/format';
  import type { User } from '../lib/types';

  const perPage = 20;
  const users = new Resource((signal) => api.get<User[]>('/users', { signal }));

  let query = $state(router.query.get('q') ?? '');
  let page = $state(1);
  let drawer = $state<{ open: boolean; user: User | null }>({ open: router.query.get('new') === '1', user: null });
  let impersonating = $state<string | null>(null);
  let settingPassword = $state<string | null>(null);
  let suspending = $state<User | null>(null);
  let deleting = $state<User | null>(null);

  $effect(() => router.setQuery({ q: query.trim() || null }));

  const self = $derived(session.me?.actor.username);
  const filtered = $derived.by(() => {
    const q = query.trim().toLowerCase();
    const list = users.data ?? [];
    return q ? list.filter((u) => u.username.includes(q)) : list;
  });
  const pageCount = $derived(Math.max(1, Math.ceil(filtered.length / perPage)));
  const visible = $derived(filtered.slice((page - 1) * perPage, page * perPage));

  $effect(() => {
    void query;
    page = 1;
  });
  $effect(() => {
    if (page > pageCount) page = pageCount;
  });

  function upsert(u: User) {
    const list = users.data ?? [];
    const i = list.findIndex((x) => x.username === u.username);
    users.set(i >= 0 ? list.with(i, u) : [...list, u].sort((a, b) => a.username.localeCompare(b.username)));
  }

  async function setSuspended(u: User, suspended: boolean) {
    const updated = await api.post<User>(`/users/${seg(u.username)}/suspend`, { suspended });
    upsert(updated);
    toast.success(suspended ? `${u.username} is suspended.` : `${u.username} is active again.`);
  }

  function sitesLabel(u: User): string {
    return u.max_sites < 0 ? String(u.site_count) : `${u.site_count} / ${u.max_sites}`;
  }
</script>

<PageHeader title="Users" description="Accounts that sign in to the panel and own sites, databases and cron jobs.">
  {#snippet actions()}
    <Button variant="primary" icon={Plus} onclick={() => (drawer = { open: true, user: null })}>New user</Button>
  {/snippet}
</PageHeader>

{#if users.loading}
  <Skeleton rows={5} />
{:else if users.error}
  <ErrorState message={users.error} onretry={() => users.reload()} />
{:else if (users.data?.length ?? 0) === 0}
  <EmptyState icon={UsersIcon} title="No users" description="Create an account for each customer or project." />
{:else}
  <div class="stack">
    <div class="spread">
      <SearchInput bind:value={query} placeholder="Search usernames" label="Search users" />
      <p class="muted small">{plural(users.data?.length ?? 0, 'user')}</p>
    </div>
    {#if filtered.length === 0}
      <EmptyState icon={SearchX} title="No matching users" description="No username contains “{query.trim()}”." compact>
        <Button onclick={() => (query = '')}>Clear search</Button>
      </EmptyState>
    {:else}
      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr>
              <th>User</th>
              <th>Sites</th>
              <th>Memory</th>
              <th>CPU</th>
              <th>Created</th>
              <th class="actions"><span class="sr-only">Actions</span></th>
            </tr>
          </thead>
          <tbody>
            {#each visible as u (u.username)}
              <tr>
                <td class="primary-cell">
                  <div class="row user">
                    <span class="cell-title mono">{u.username}</span>
                    {#if u.username === self}<span class="muted small">(you)</span>{/if}
                    {#if u.is_admin}<Badge strong>Admin</Badge>{/if}
                    {#if u.is_suspended}<Badge>Suspended</Badge>{/if}
                  </div>
                </td>
                <td data-label="Sites" class="nowrap">{sitesLabel(u)}</td>
                <td data-label="Memory" class="nowrap">{megabytes(u.memory_mb)}</td>
                <td data-label="CPU" class="nowrap">{limit(u.cpu_percent, '%')}</td>
                <td data-label="Created" class="muted nowrap">{date(u.created_at)}</td>
                <td class="actions">
                  <div class="row nowrap">
                    <Button size="sm" icon={Pencil} onclick={() => (drawer = { open: true, user: u })}>Edit</Button>
                    <Menu
                      label="More actions for {u.username}"
                      items={[
                        {
                          label: 'Sign in as user',
                          icon: LogIn,
                          hidden: u.is_admin || u.is_suspended || u.username === self,
                          onclick: () => (impersonating = u.username),
                        },
                        { label: 'Set password', icon: KeyRound, onclick: () => (settingPassword = u.username) },
                        u.is_suspended
                          ? { label: 'Unsuspend', icon: CircleCheck, onclick: () => (suspending = u) }
                          : { label: 'Suspend', icon: Ban, hidden: u.username === self, onclick: () => (suspending = u) },
                        {
                          label: 'Delete user',
                          icon: Trash2,
                          destructive: true,
                          hidden: u.username === self,
                          onclick: () => (deleting = u),
                        },
                      ]}
                    />
                  </div>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
      {#if pageCount > 1}<Pagination bind:page total={filtered.length} {perPage} />{/if}
    {/if}
  </div>
{/if}

<UserDrawer
  open={drawer.open}
  user={drawer.user}
  onclose={() => {
    drawer = { open: false, user: null };
    router.setQuery({ new: null });
  }}
  onsaved={upsert}
/>

<ImpersonateDialog username={impersonating} onclose={() => (impersonating = null)} />
<SetPasswordDialog username={settingPassword} onclose={() => (settingPassword = null)} />

<ConfirmDialog
  open={suspending !== null}
  title={suspending?.is_suspended ? `Unsuspend ${suspending?.username}?` : `Suspend ${suspending?.username}?`}
  message={suspending?.is_suspended
    ? 'Their sites come back online, cron jobs run again and they can sign in.'
    : 'Their sites show a suspension page, cron jobs stop, SSH and SFTP logins are refused and they are signed out of the panel. Files and databases are kept.'}
  actionLabel={suspending?.is_suspended ? 'Unsuspend' : 'Suspend'}
  destructive={!suspending?.is_suspended}
  onclose={() => (suspending = null)}
  onconfirm={async () => {
    if (suspending) await setSuspended(suspending, !suspending.is_suspended);
  }}
/>

<ConfirmDialog
  open={deleting !== null}
  title="Delete {deleting?.username}?"
  message="This permanently removes the Linux account, its home folder with every site, all of its databases and its cron jobs. Backups at external destinations are kept."
  confirmText={deleting?.username}
  actionLabel="Delete user"
  onclose={() => (deleting = null)}
  onconfirm={async () => {
    if (!deleting) return;
    const name = deleting.username;
    await api.del(`/users/${seg(name)}`);
    users.set((users.data ?? []).filter((u) => u.username !== name));
    toast.success(`${name} deleted.`);
  }}
/>

<style>
  .user {
    gap: 8px;
    flex-wrap: wrap;
  }
</style>
