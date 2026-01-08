---
name: CI artifact permissions
about: Notes for ensuring GitHub Actions can manage artifacts (delete/upload) without 403 errors
---

If your workflow fails with `gh: Resource not accessible by integration (HTTP 403)` when attempting to delete or manage artifacts, ensure the workflow permissions are set correctly.

Actions:
- Add a `permissions` stanza to the workflow (top-level) to grant the workflow `actions: write` (and if creating releases or uploading content, `contents: write`):

```yaml
permissions:
  actions: write
  contents: write
```

- If you're using the GitHub CLI (`gh`) to manage artifacts, ensure the token you pass to `gh auth login` has sufficient scopes. The default `GITHUB_TOKEN` has limited scopes for security; if needed create a Personal Access Token with the `workflow` and `repo` scopes and store it in a GitHub secret (e.g., `PAT_TOKEN`) and use it in the workflow.

- Prefer job-level scoping when you only need elevated permissions for one job:

```yaml
jobs:
  release:
    permissions:
      actions: write
      contents: write
    steps:
      - name: Delete artifacts
        run: gh ...
```

If you'd like, I can add an optional job-level PAT usage example (using `secrets.PAT_TOKEN`) to the release job; say the word and I'll add it.