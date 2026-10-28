import { api } from './api';

/** Starts a browser download of a finished export job. */
export async function downloadExport(jobId: string): Promise<void> {
  const { url } = await api.post<{ url: string }>(`/backups/jobs/${encodeURIComponent(jobId)}/download-link`);
  const a = document.createElement('a');
  a.href = url;
  a.download = '';
  a.rel = 'noopener';
  document.body.appendChild(a);
  a.click();
  a.remove();
}
