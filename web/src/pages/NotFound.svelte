<script lang="ts">
  import Compass from '@lucide/svelte/icons/compass';
  import EmptyState from '../components/EmptyState.svelte';
  import Button from '../components/Button.svelte';
</script>

<EmptyState icon={Compass} title="Page not found" description="This page does not exist, or you do not have access to it.">
  <Button href="/" variant="primary">Go to overview</Button>
</EmptyState>
