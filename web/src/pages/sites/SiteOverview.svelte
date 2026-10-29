<script lang="ts">
  import ExternalLink from '@lucide/svelte/icons/external-link';
  import Card from '../../components/Card.svelte';
  import CopyField from '../../components/CopyField.svelte';
  import Button from '../../components/Button.svelte';
  import { session } from '../../lib/session.svelte';
  import { dateTime, siteType } from '../../lib/format';
  import type { Site } from '../../lib/types';

  let { site }: { site: Site } = $props();

  const host = $derived(session.meta?.hostname || location.hostname);
  const port = $derived(session.meta?.ssh_port ?? 22);
  const folder = $derived(site.document_root.replace(/\/public$/, ''));
  const proxy = $derived(site.site_type === 'proxy');
</script>

<div class="grid-2">
  <Card title="Details">
    <dl class="kv">
      <dt>Primary domain</dt>
      <dd>{site.domain}</dd>
      <dt>Other domains</dt>
      <dd>{site.domains.length > 1 ? site.domains.filter((d) => !d.is_primary).map((d) => d.domain).join(', ') : 'None'}</dd>
      <dt>Type</dt>
      <dd>{siteType(site.site_type)}</dd>
      {#if proxy}
        <dt>Application</dt>
        <dd class="mono">{site.proxy_url}</dd>
      {:else}
        <dt>PHP</dt>
        <dd class="mono">{site.php_version}</dd>
      {/if}
      <dt>HTTPS redirect</dt>
      <dd>{site.force_https ? 'On' : 'Off'}</dd>
      <dt>Created</dt>
      <dd>{dateTime(site.created_at)}</dd>
    </dl>
    {#if site.site_type === 'wordpress'}
      <div class="wp">
        <Button href="https://{site.domain}/wp-admin/" external icon={ExternalLink} size="sm">WordPress admin</Button>
      </div>
    {/if}
  </Card>

  <Card
    title="Files"
    description={proxy
      ? 'Upload your application with SFTP. Visitors reach it only through the web server, never the files directly.'
      : 'Upload your code with SFTP. Files in public/ are served on the web.'}
  >
    <div class="stack-sm">
      {#if !proxy}<CopyField label="Web root" value={site.document_root} />{/if}
      <CopyField label="Site folder" value={folder} />
      <CopyField label="SFTP" value="sftp://{site.username}@{host}:{port}{folder}" />
      <p class="muted small">
        Sign in as <span class="mono">{site.username}</span> with your FastCP password or an SSH key.
        {#if site.site_type === 'wordpress'}wp-config.php is stored in the site folder, outside the web root.{/if}
      </p>
    </div>
  </Card>
</div>

{#if session.isAdmin}
  <div class="gap">
    <Card
      title="Custom web server configuration"
      description="As root, add your own Caddy directives for this site to this file. FastCP never changes it."
    >
      <div class="stack-sm">
        <CopyField label="File" value="/opt/fastcp/config/caddy.d/sites/{site.username}/{site.slug}.caddy" />
        <p class="muted small">Changes take effect when you apply them under <a href="/settings/webserver">Settings → Web server</a>.</p>
      </div>
    </Card>
  </div>
{/if}

<style>
  .wp {
    margin-top: 16px;
  }
  .gap {
    margin-top: 24px;
  }
</style>
