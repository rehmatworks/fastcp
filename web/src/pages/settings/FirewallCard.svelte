<script lang="ts">
  import Download from '@lucide/svelte/icons/download';
  import Plus from '@lucide/svelte/icons/plus';
  import Trash2 from '@lucide/svelte/icons/trash-2';
  import Lock from '@lucide/svelte/icons/lock';
  import Card from '../../components/Card.svelte';
  import Button from '../../components/Button.svelte';
  import Input from '../../components/Input.svelte';
  import Select from '../../components/Select.svelte';
  import Switch from '../../components/Switch.svelte';
  import Badge from '../../components/Badge.svelte';
  import Skeleton from '../../components/Skeleton.svelte';
  import ErrorState from '../../components/ErrorState.svelte';
  import ConfirmDialog from '../../components/ConfirmDialog.svelte';
  import { api, errorMessage, isAbort } from '../../lib/api';
  import { Resource, lifetime } from '../../lib/page.svelte';
  import { pollJob } from '../../lib/jobs';
  import { toast } from '../../lib/toast.svelte';
  import type { AgentJob, FirewallRule, FirewallStatus } from '../../lib/types';

  let { revision }: { revision: number } = $props();
  const signal = lifetime();
  const fw = new Resource((sig) => api.get<FirewallStatus>('/settings/firewall', { signal: sig }));

  let seen = 0;
  $effect(() => {
    if (revision !== seen) {
      seen = revision;
      if (revision > 0) void fw.reload({ quiet: true });
    }
  });

  let installing = $state(false);
  let toggling = $state(false);
  let confirmEnable = $state(false);
  let adding = $state(false);
  let addError = $state<string | null>(null);
  let removing = $state<FirewallRule | null>(null);
  let rule = $state({ action: 'allow', port: '' as string | number, protocol: 'tcp', ip_version: 'both' });

  const portNum = $derived(Number(rule.port));
  const portErr = $derived(rule.port === '' || (Number.isInteger(portNum) && portNum >= 1 && portNum <= 65535) ? null : 'Between 1 and 65535.');
  const protectedPort = $derived(rule.action === 'deny' && (fw.data?.protected_ports ?? []).includes(portNum));

  async function install() {
    installing = true;
    try {
      const job = await api.post<AgentJob>('/settings/firewall/install');
      const done = await pollJob((sig) => api.get<AgentJob>(`/jobs/${job.id}`, { signal: sig }), { signal });
      if (done.status === 'succeeded') toast.success(done.message || 'The firewall is installed.');
      else toast.error(done.message || 'The firewall could not be installed.');
      await fw.reload({ quiet: true });
    } catch (err) {
      if (!isAbort(err)) toast.error(errorMessage(err));
    } finally {
      installing = false;
    }
  }

  async function setEnabled(enabled: boolean) {
    toggling = true;
    try {
      fw.set(await api.put<FirewallStatus>('/settings/firewall/enabled', { enabled }));
      toast.success(enabled ? 'The firewall is on.' : 'The firewall is off.');
    } catch (err) {
      toast.error(errorMessage(err));
      await fw.reload({ quiet: true });
    } finally {
      toggling = false;
    }
  }

  async function addRule(e: Event) {
    e.preventDefault();
    if (portErr || protectedPort || rule.port === '') return;
    adding = true;
    addError = null;
    try {
      fw.set(await api.post<FirewallStatus>('/settings/firewall/rules', { ...rule, port: portNum }));
      toast.success(`${rule.action === 'allow' ? 'Allowed' : 'Blocked'} port ${portNum}/${rule.protocol}.`);
      rule.port = '';
    } catch (err) {
      addError = errorMessage(err);
    } finally {
      adding = false;
    }
  }

  function ipLabel(v: string): string {
    return v === 'ipv6' ? 'IPv6' : v === 'ipv4' ? 'IPv4' : 'IPv4 + IPv6';
  }
</script>

{#if fw.loading}
  <Skeleton variant="card" rows={2} />
{:else if fw.error || !fw.data}
  <ErrorState message={fw.error ?? 'Unknown error'} onretry={() => fw.reload()} />
{:else}
  {@const st = fw.data}
  <Card title="Firewall" description="Incoming traffic is filtered with ufw. Ports for the websites, the panel and SSH always stay open.">
    {#snippet actions()}
      {#if st.installed && st.available}
        <Switch
          label="Firewall enabled"
          hideLabel
          checked={st.enabled}
          disabled={toggling}
          onchange={(on) => {
            if (on) confirmEnable = true;
            else void setEnabled(false);
          }}
        />
      {/if}
    {/snippet}

    {#if !st.installed}
      <div class="row">
        <p class="grow">ufw is not installed.</p>
        <Button icon={Download} loading={installing} onclick={install}>Install firewall</Button>
      </div>
    {:else if !st.available}
      <p class="notice strong">{st.message ?? 'The firewall cannot be managed on this server.'}</p>
    {:else}
      <div class="stack">
        {#if !st.enabled}
          <p class="notice">The firewall is installed but off. Rules take effect once it is enabled.</p>
        {/if}
        {#if st.rules.length}
          <div class="table-wrap">
            <table class="table">
              <thead>
                <tr>
                  <th>Port</th>
                  <th>Action</th>
                  <th>From</th>
                  <th>IP version</th>
                  <th class="actions"><span class="sr-only">Actions</span></th>
                </tr>
              </thead>
              <tbody>
                {#each st.rules as r, i (i)}
                  <tr>
                    <td class="primary-cell">
                      <span class="mono cell-title">{r.port ? `${r.port}/${r.protocol}` : r.raw}</span>
                    </td>
                    <td data-label="Action">
                      {#if r.action === 'ALLOW'}<Badge>Allow</Badge>{:else}<Badge strong>{r.action === 'DENY' ? 'Deny' : r.action}</Badge>{/if}
                    </td>
                    <td data-label="From" class="mono">{r.from}</td>
                    <td data-label="IP version">{ipLabel(r.ip_version)}</td>
                    <td class="actions">
                      {#if r.protected}
                        <span class="muted small protected" title="Required by the websites, the panel or SSH">
                          <Lock size={13} strokeWidth={1.75} aria-hidden="true" /> Required
                        </span>
                      {:else if r.port && !st.protected_ports.includes(r.port)}
                        <Button
                          size="sm"
                          variant="ghost"
                          icon={Trash2}
                          iconOnly
                          aria-label="Remove rule for port {r.port}/{r.protocol}"
                          onclick={() => (removing = r)}
                        />
                      {/if}
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {:else}
          <p class="muted">No rules yet.</p>
        {/if}

        <form class="add" onsubmit={addRule}>
          <Select
            label="Action"
            bind:value={rule.action}
            options={[
              { value: 'allow', label: 'Allow' },
              { value: 'deny', label: 'Deny' },
            ]}
          />
          <Input label="Port" type="number" bind:value={rule.port} min={1} max={65535} error={portErr} placeholder="8080" />
          <Select
            label="Protocol"
            bind:value={rule.protocol}
            options={[
              { value: 'tcp', label: 'TCP' },
              { value: 'udp', label: 'UDP' },
            ]}
          />
          <Select
            label="IP version"
            bind:value={rule.ip_version}
            options={[
              { value: 'both', label: 'IPv4 + IPv6' },
              { value: 'ipv4', label: 'IPv4' },
              { value: 'ipv6', label: 'IPv6' },
            ]}
          />
          <div class="submit">
            <Button type="submit" icon={Plus} loading={adding} disabled={rule.port === '' || !!portErr || protectedPort}>Add rule</Button>
          </div>
        </form>
        {#if protectedPort}<p class="muted small">Port {portNum} is required by FastCP or SSH and cannot be blocked.</p>{/if}
        {#if addError}<p class="notice strong" role="alert">{addError}</p>{/if}
      </div>
    {/if}
  </Card>

  <ConfirmDialog
    open={confirmEnable}
    title="Turn on the firewall?"
    message="Incoming connections are refused except on ports with an allow rule. Ports {st.protected_ports.join(
      ', ',
    )} (websites, panel and SSH) are opened first so you are not locked out."
    actionLabel="Turn on"
    destructive={false}
    onclose={() => (confirmEnable = false)}
    onconfirm={() => setEnabled(true)}
  />

  <ConfirmDialog
    open={removing !== null}
    title="Remove the rule for port {removing?.port}/{removing?.protocol}?"
    message="Matching allow and deny rules for this port are removed."
    actionLabel="Remove rule"
    onclose={() => (removing = null)}
    onconfirm={async () => {
      if (!removing) return;
      fw.set(
        await api.post<FirewallStatus>('/settings/firewall/rules/delete', {
          port: removing.port,
          protocol: removing.protocol,
          ip_version: removing.ip_version,
        }),
      );
      toast.success('Rule removed.');
    }}
  />
{/if}

<style>
  .grow {
    flex: 1;
  }
  .add {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr)) auto;
    gap: 12px;
    align-items: start;
  }
  .submit {
    padding-top: 22px;
  }
  .protected {
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }
  @media (max-width: 720px) {
    .add {
      grid-template-columns: 1fr 1fr;
    }
    .submit {
      padding-top: 0;
      grid-column: 1 / -1;
    }
  }
</style>
