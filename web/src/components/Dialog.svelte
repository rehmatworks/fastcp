<script lang="ts">
  import type { Snippet } from 'svelte';
  import Modal from './Modal.svelte';

  interface Props {
    open: boolean;
    title: string;
    description?: string;
    size?: 'sm' | 'md' | 'lg';
    busy?: boolean;
    onclose: () => void;
    children: Snippet;
    footer?: Snippet;
  }

  let props: Props = $props();
</script>

<Modal kind="dialog" {...props} />
