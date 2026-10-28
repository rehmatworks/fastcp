import type { AgentJobStatus, BackupJobStatus, PHPSupport, SSLStatus, Site } from './types';
import { date } from './format';

export type StatusKind = 'ok' | 'error' | 'warn' | 'pending' | 'running' | 'off' | 'info';

export interface StatusView {
  kind: StatusKind;
  label: string;
  title?: string;
}

const sslLabels: Record<string, StatusView> = {
  valid: { kind: 'ok', label: 'Secure' },
  pending: { kind: 'pending', label: 'Issuing' },
  unreachable: { kind: 'warn', label: 'Not reachable' },
  mismatch: { kind: 'warn', label: 'Wrong certificate' },
  untrusted: { kind: 'warn', label: 'Untrusted' },
  expired: { kind: 'error', label: 'Expired' },
};

export function sslView(st: SSLStatus | undefined): StatusView {
  if (!st) return { kind: 'pending', label: 'Checking' };
  const v = sslLabels[st.status] ?? { kind: 'info', label: st.status };
  const title = st.valid && st.expires_at ? `Valid until ${date(st.expires_at)}` : st.message;
  return { ...v, title };
}

/** Summarises SSL across a site's domains. */
export function siteSSL(site: Site): StatusView {
  const states = site.domains.map((d) => site.ssl[d.domain]);
  if (states.length === 0) return { kind: 'off', label: 'No domains' };
  if (states.every((s) => s?.valid)) return { kind: 'ok', label: 'Secure' };
  const bad = states.find((s) => s && !s.valid && s.status !== 'pending');
  if (bad) {
    const v = sslView(bad);
    return { ...v, label: states.length > 1 ? `${v.label} (some domains)` : v.label };
  }
  return { kind: 'pending', label: states.some((s) => !s) ? 'Checking' : 'Issuing' };
}

export function backupJobView(status: BackupJobStatus | string): StatusView {
  switch (status) {
    case 'running':
      return { kind: 'running', label: 'Running' };
    case 'success':
      return { kind: 'ok', label: 'Succeeded' };
    case 'failed':
      return { kind: 'error', label: 'Failed' };
    case 'idle':
    case '':
      return { kind: 'off', label: 'Never run' };
    default:
      return { kind: 'info', label: status };
  }
}

export function agentJobView(status: AgentJobStatus | string): StatusView {
  switch (status) {
    case 'queued':
      return { kind: 'pending', label: 'Queued' };
    case 'running':
      return { kind: 'running', label: 'Running' };
    case 'succeeded':
      return { kind: 'ok', label: 'Succeeded' };
    case 'failed':
      return { kind: 'error', label: 'Failed' };
    default:
      return { kind: 'info', label: status };
  }
}

export function phpSupportView(status: PHPSupport, eol?: string): StatusView {
  switch (status) {
    case 'active':
      return { kind: 'ok', label: 'Active support', title: eol ? `Supported until ${date(eol)}` : undefined };
    case 'security':
      return { kind: 'info', label: 'Security fixes only', title: eol ? `End of life ${date(eol)}` : undefined };
    case 'eol':
      return { kind: 'warn', label: 'End of life', title: eol ? `Ended ${date(eol)}` : undefined };
    default:
      return { kind: 'off', label: 'Unknown' };
  }
}

/** Job types that create a snapshot. */
export const backupRunTypes = new Set(['manual', 'scheduled']);

export const jobTypeLabels: Record<string, string> = {
  manual: 'Backup',
  scheduled: 'Scheduled backup',
  restore_site: 'Restore site',
  restore_database: 'Restore database',
  export: 'Prepare download',
  delete_snapshot: 'Delete snapshot',
};
