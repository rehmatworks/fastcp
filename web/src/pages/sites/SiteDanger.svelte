<script lang="ts">
  import Trash2 from '@lucide/svelte/icons/trash-2';
  import Card from '../../components/Card.svelte';
  import Button from '../../components/Button.svelte';
  import ConfirmDialog from '../../components/ConfirmDialog.svelte';
  import { api, seg } from '../../lib/api';
  import { router } from '../../lib/router.svelte';
  import { toast } from '../../lib/toast.svelte';
  import type { Site } from '../../lib/types';

  let { site }: { site: Site } = $props();
  let confirming = $state(false);
</script>

<Card title="Delete this site">
  <div class="spread">
    <p class="muted text">
      Removes {site.domain}{site.domains.length > 1 ? ` and ${site.domains.length - 1} other domain${site.domains.length > 2 ? 's' : ''}` : ''},
      the PHP pool and every file in the site folder. Databases are kept; delete them separately on the Databases page. Existing backups are kept.
    </p>
    <Button variant="danger" icon={Trash2} onclick={() => (confirming = true)}>Delete site</Button>
  </div>
</Card>

<ConfirmDialog
  open={confirming}
  title="Delete {site.domain}?"
  message="This permanently deletes the site’s files. It cannot be undone."
  confirmText={site.domain}
  actionLabel="Delete site"
  onclose={() => (confirming = false)}
  onconfirm={async () => {
    await api.del(`/sites/${seg(site.id)}`);
    toast.success(`${site.domain} was deleted.`);
    router.navigate('/sites', { force: true });
  }}
/>

<style>
  .text {
    max-width: 620px;
  }
</style>
