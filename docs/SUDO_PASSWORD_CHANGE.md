# Enabling API-driven Password Changes via sudo

FastCP can, optionally, perform system password changes using `sudo chpasswd` when the panel is running as a non-root process. This is **disabled by default** and must be explicitly enabled in `.fastcp/config.json` by setting:

```json
{
  "allow_sudo_password_change": true
}
```

## Security and sudoers configuration

For this to work securely without prompting for a password, add a tightly-scoped sudoers rule that only permits running `/usr/sbin/chpasswd` as root, in a non-interactive way. Example (edit with `visudo -f /etc/sudoers.d/fastcp-chpasswd`):

```
# Allow the fastcp process user to run chpasswd without a password
fastcpuser ALL=(root) NOPASSWD: /usr/sbin/chpasswd
```

Replace `fastcpuser` with the system user that runs the FastCP process (commonly `fastcp` or the service user). Ensure the sudoers file is owned by `root:root` and permission `0440`.

**Important security notes:**
- Limit sudoers to the exact path `/usr/sbin/chpasswd` to reduce risk.
- Use `NOPASSWD` only for that specific command; do not allow a general `NOPASSWD: ALL` entry.
- Keep the FastCP server patched and limit access to its admin UI.

## Verifying sudo configuration

A helper script is provided at `tools/check_sudo_chpasswd` (Go binary) that checks whether `sudo` is available non-interactively and whether `/usr/sbin/chpasswd` appears in `sudo -l` output (indicating it's allowed). Use it like:

```bash
# build and run
cd tools/check_sudo_chpasswd
go build -o check_sudo_chpasswd
sudo -n true || echo "sudo requires password"
./check_sudo_chpasswd
```

If the tool reports that `chpasswd` is allowed, you can enable the `allow_sudo_password_change` flag in your config and FastCP will attempt `sudo chpasswd` when needed.

## Manual alternative

If you prefer not to enable sudo-based password changes, change system passwords manually as root:

```bash
# change 'supertest' password
echo "supertest:NEWPASSWORD" | sudo chpasswd
```

This is a safe fallback and recommended if you do not want to run FastCP as root or configure sudo privileges.
