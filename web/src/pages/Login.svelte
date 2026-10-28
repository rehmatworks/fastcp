<script lang="ts">
  import Logo from '../components/Logo.svelte';
  import Button from '../components/Button.svelte';
  import Input from '../components/Input.svelte';
  import { session } from '../lib/session.svelte';
  import { router } from '../lib/router.svelte';
  import { api, errorMessage } from '../lib/api';

  let username = $state('');
  let password = $state('');
  let busy = $state(false);
  let error = $state<string | null>(null);
  let version = $state('');
  let userInput: HTMLInputElement | HTMLTextAreaElement | null = $state(null);

  $effect(() => {
    userInput?.focus();
  });

  $effect(() => {
    const c = new AbortController();
    api
      .get<{ version: string }>('/version', { signal: c.signal, anonymous: true })
      .then((v) => (version = v.version))
      .catch(() => {});
    return () => c.abort();
  });

  function safeNext(): string {
    const next = router.query.get('next') ?? '/';
    return next.startsWith('/') && !next.startsWith('//') && !next.startsWith('/login') ? next : '/';
  }

  async function submit(e: Event) {
    e.preventDefault();
    busy = true;
    error = null;
    try {
      await session.login(username.trim(), password);
      router.navigate(safeNext(), { replace: true, force: true });
    } catch (err) {
      error = errorMessage(err);
      password = '';
    } finally {
      busy = false;
    }
  }
</script>

<div class="login">
  <div class="panel">
    <div class="brand"><Logo size={30} /></div>
    <div class="intro">
      <h1>Sign in</h1>
      <p class="muted">Use your server account to manage your sites.</p>
    </div>
    <form class="stack" onsubmit={submit}>
      <Input
        label="Username"
        bind:value={username}
        autocomplete="username"
        autocapitalize="off"
        spellcheck={false}
        required
        bind:input={userInput}
      />
      <Input label="Password" type="password" bind:value={password} autocomplete="current-password" required />
      {#if error}<p class="notice strong" role="alert">{error}</p>{/if}
      <Button type="submit" variant="primary" full loading={busy} disabled={!username || !password}>Sign in</Button>
    </form>
  </div>
  {#if version}<p class="version muted small">FastCP {version}</p>{/if}
</div>

<style>
  .login {
    min-height: 100vh;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 20px;
    padding: 24px 16px;
    background: var(--bg-subtle);
  }
  .panel {
    width: min(380px, 100%);
    display: flex;
    flex-direction: column;
    gap: 24px;
    padding: 32px 28px 28px;
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    background: var(--bg);
    box-shadow: var(--shadow-sm);
  }
  .intro {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .version {
    font-family: var(--mono);
  }
</style>
