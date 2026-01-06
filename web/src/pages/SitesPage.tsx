import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  Globe,
  Plus,
  Search,
  MoreVertical,
  Trash2,
  ExternalLink,
  PauseCircle,
  PlayCircle,
  RefreshCw,
  FolderOpen,
  AlertCircle,
  Loader2,
} from 'lucide-react'
import { api } from '@/lib/api'
import { formatDate, getStatusBgColor, cn } from '@/lib/utils'
import type { Site } from '@/types'

interface ConfirmModalProps {
  isOpen: boolean
  title: string
  message: string
  confirmLabel: string
  confirmVariant?: 'danger' | 'warning' | 'primary'
  isLoading?: boolean
  onConfirm: () => void
  onCancel: () => void
}

function ConfirmModal({
  isOpen,
  title,
  message,
  confirmLabel,
  confirmVariant = 'danger',
  isLoading,
  onConfirm,
  onCancel,
}: ConfirmModalProps) {
  if (!isOpen) return null

  const variantClasses = {
    danger: 'bg-red-500 hover:bg-red-600 text-white',
    warning: 'bg-amber-500 hover:bg-amber-600 text-white',
    primary: 'bg-primary hover:bg-primary/90 text-primary-foreground',
  }

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
      <div className="bg-card border border-border rounded-2xl w-full max-w-md shadow-xl">
        <div className="p-6">
          <div className="flex items-start gap-4">
            <div className={cn(
              "w-12 h-12 rounded-xl flex items-center justify-center flex-shrink-0",
              confirmVariant === 'danger' && "bg-red-500/10",
              confirmVariant === 'warning' && "bg-amber-500/10",
              confirmVariant === 'primary' && "bg-primary/10",
            )}>
              <AlertCircle className={cn(
                "w-6 h-6",
                confirmVariant === 'danger' && "text-red-500",
                confirmVariant === 'warning' && "text-amber-500",
                confirmVariant === 'primary' && "text-primary",
              )} />
            </div>
            <div className="flex-1">
              <h3 className="text-lg font-semibold">{title}</h3>
              <p className="text-sm text-muted-foreground mt-1">{message}</p>
            </div>
          </div>
        </div>
        <div className="flex gap-3 px-6 pb-6">
          <button
            onClick={onCancel}
            disabled={isLoading}
            className="flex-1 px-4 py-2.5 bg-secondary hover:bg-secondary/80 rounded-xl font-medium transition-colors disabled:opacity-50"
          >
            Cancel
          </button>
          <button
            onClick={onConfirm}
            disabled={isLoading}
            className={cn(
              "flex-1 flex items-center justify-center gap-2 px-4 py-2.5 rounded-xl font-medium transition-colors disabled:opacity-50",
              variantClasses[confirmVariant]
            )}
          >
            {isLoading ? (
              <>
                <Loader2 className="w-4 h-4 animate-spin" />
                Processing...
              </>
            ) : (
              confirmLabel
            )}
          </button>
        </div>
      </div>
    </div>
  )
}

export function SitesPage() {
  const [sites, setSites] = useState<Site[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [search, setSearch] = useState('')
  const [openMenu, setOpenMenu] = useState<string | null>(null)
  const [deleteConfirm, setDeleteConfirm] = useState<Site | null>(null)
  const [deleting, setDeleting] = useState(false)

  useEffect(() => {
    fetchSites()
  }, [])

  async function fetchSites() {
    try {
      const data = await api.getSites()
      setSites(data.sites || [])
    } catch (error) {
      console.error('Failed to fetch sites:', error)
    } finally {
      setIsLoading(false)
    }
  }

  async function handleSuspend(id: string) {
    try {
      await api.suspendSite(id)
      fetchSites()
    } catch (error) {
      console.error('Failed to suspend site:', error)
    }
    setOpenMenu(null)
  }

  async function handleUnsuspend(id: string) {
    try {
      await api.unsuspendSite(id)
      fetchSites()
    } catch (error) {
      console.error('Failed to unsuspend site:', error)
    }
    setOpenMenu(null)
  }

  async function handleDelete() {
    if (!deleteConfirm) return
    setDeleting(true)
    try {
      await api.deleteSite(deleteConfirm.id)
      setDeleteConfirm(null)
      fetchSites()
    } catch (error) {
      console.error('Failed to delete site:', error)
    } finally {
      setDeleting(false)
    }
  }

  async function handleRestartWorkers(id: string) {
    try {
      await api.restartSiteWorkers(id)
    } catch (error) {
      console.error('Failed to restart workers:', error)
    }
    setOpenMenu(null)
  }

  const filteredSites = sites.filter(
    (site) =>
      site.name.toLowerCase().includes(search.toLowerCase()) ||
      site.domain.toLowerCase().includes(search.toLowerCase())
  )

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="w-10 h-10 border-2 border-primary border-t-transparent rounded-full animate-spin" />
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Sites</h1>
          <p className="text-muted-foreground mt-1">
            Manage your websites and applications
          </p>
        </div>
        <Link
          to="/sites/new"
          className="flex items-center justify-center gap-2 px-4 py-2.5 bg-primary hover:bg-primary/90 text-primary-foreground font-medium rounded-xl transition-colors"
        >
          <Plus className="w-4 h-4" />
          New Site
        </Link>
      </div>

      {/* Search */}
      <div className="relative">
        <Search className="absolute left-4 top-1/2 -translate-y-1/2 w-5 h-5 text-muted-foreground" />
        <input
          type="text"
          placeholder="Search sites..."
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="w-full pl-12 pr-4 py-3 bg-card border border-border rounded-xl focus:outline-none focus:ring-2 focus:ring-primary/50 focus:border-primary transition-colors"
        />
      </div>

      {/* Sites List */}
      {filteredSites.length === 0 ? (
        <div className="bg-card border border-border rounded-2xl p-12 text-center card-shadow">
          <div className="w-16 h-16 rounded-2xl bg-secondary flex items-center justify-center mx-auto mb-4">
            {sites.length === 0 ? (
              <FolderOpen className="w-8 h-8 text-muted-foreground" />
            ) : (
              <Search className="w-8 h-8 text-muted-foreground" />
            )}
          </div>
          <h3 className="font-semibold text-lg mb-2">
            {sites.length === 0 ? 'No sites yet' : 'No sites found'}
          </h3>
          <p className="text-muted-foreground mb-6 max-w-sm mx-auto">
            {sites.length === 0
              ? 'Create your first site to start hosting your applications'
              : 'Try a different search term'}
          </p>
          {sites.length === 0 && (
            <Link
              to="/sites/new"
              className="inline-flex items-center gap-2 px-4 py-2.5 bg-primary hover:bg-primary/90 text-primary-foreground font-medium rounded-xl transition-colors"
            >
              <Plus className="w-4 h-4" />
              Create Site
            </Link>
          )}
        </div>
      ) : (
        <div className="bg-card border border-border rounded-2xl overflow-hidden card-shadow">
          <div className="overflow-x-auto">
            <table className="w-full">
              <thead>
                <tr className="border-b border-border bg-secondary/50">
                  <th className="text-left px-5 py-3 text-xs font-medium text-muted-foreground uppercase tracking-wider">
                    Site
                  </th>
                  <th className="text-left px-5 py-3 text-xs font-medium text-muted-foreground uppercase tracking-wider">
                    PHP
                  </th>
                  <th className="text-left px-5 py-3 text-xs font-medium text-muted-foreground uppercase tracking-wider">
                    Status
                  </th>
                  <th className="text-left px-5 py-3 text-xs font-medium text-muted-foreground uppercase tracking-wider">
                    Created
                  </th>
                  <th className="text-right px-5 py-3 text-xs font-medium text-muted-foreground uppercase tracking-wider">
                    Actions
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {filteredSites.map((site) => (
                  <tr key={site.id} className="hover:bg-secondary/30 transition-colors">
                    <td className="px-5 py-4">
                      <Link
                        to={`/sites/${site.id}`}
                        className="flex items-center gap-3"
                      >
                        <div className="w-10 h-10 rounded-xl bg-emerald-500/10 flex items-center justify-center">
                          <Globe className="w-5 h-5 text-emerald-600 dark:text-emerald-400" />
                        </div>
                        <div>
                          <p className="font-medium hover:text-primary transition-colors">
                            {site.name}
                          </p>
                          <p className="text-sm text-muted-foreground">
                            {site.domain}
                          </p>
                        </div>
                      </Link>
                    </td>
                    <td className="px-5 py-4">
                      <code className="text-sm bg-secondary px-2 py-1 rounded">
                        {site.php_version}
                      </code>
                    </td>
                    <td className="px-5 py-4">
                      <span
                        className={`text-xs px-2.5 py-1 rounded-full border font-medium ${getStatusBgColor(
                          site.status
                        )}`}
                      >
                        {site.status}
                      </span>
                    </td>
                    <td className="px-5 py-4 text-sm text-muted-foreground">
                      {formatDate(site.created_at)}
                    </td>
                    <td className="px-5 py-4">
                      <div className="flex items-center justify-end gap-1">
                        <a
                          href={`https://${site.domain}`}
                          target="_blank"
                          rel="noopener noreferrer"
                          className="p-2 text-muted-foreground hover:text-foreground hover:bg-secondary rounded-lg transition-colors"
                          title="Visit site"
                        >
                          <ExternalLink className="w-4 h-4" />
                        </a>
                        <div className="relative">
                          <button
                            onClick={() =>
                              setOpenMenu(openMenu === site.id ? null : site.id)
                            }
                            className="p-2 text-muted-foreground hover:text-foreground hover:bg-secondary rounded-lg transition-colors"
                          >
                            <MoreVertical className="w-4 h-4" />
                          </button>
                          {openMenu === site.id && (
                            <>
                              <div
                                className="fixed inset-0 z-10"
                                onClick={() => setOpenMenu(null)}
                              />
                              <div className="absolute right-0 top-full mt-1 w-44 bg-card border border-border rounded-xl shadow-lg z-20 overflow-hidden">
                                <div className="py-1">
                                  {site.status === 'active' ? (
                                    <button
                                      onClick={() => handleSuspend(site.id)}
                                      className="flex items-center gap-3 w-full px-3 py-2 text-sm text-left hover:bg-secondary transition-colors"
                                    >
                                      <PauseCircle className="w-4 h-4 text-amber-500" />
                                      Suspend
                                    </button>
                                  ) : (
                                    <button
                                      onClick={() => handleUnsuspend(site.id)}
                                      className="flex items-center gap-3 w-full px-3 py-2 text-sm text-left hover:bg-secondary transition-colors"
                                    >
                                      <PlayCircle className="w-4 h-4 text-emerald-500" />
                                      Activate
                                    </button>
                                  )}
                                  {site.worker_mode && (
                                    <button
                                      onClick={() => handleRestartWorkers(site.id)}
                                      className="flex items-center gap-3 w-full px-3 py-2 text-sm text-left hover:bg-secondary transition-colors"
                                    >
                                      <RefreshCw className="w-4 h-4 text-blue-500" />
                                      Restart Workers
                                    </button>
                                  )}
                                  <div className="my-1 border-t border-border" />
                                  <button
                                    onClick={() => {
                                      setDeleteConfirm(site)
                                      setOpenMenu(null)
                                    }}
                                    className="flex items-center gap-3 w-full px-3 py-2 text-sm text-left text-red-500 hover:bg-red-500/10 transition-colors"
                                  >
                                    <Trash2 className="w-4 h-4" />
                                    Delete Site
                                  </button>
                                </div>
                              </div>
                            </>
                          )}
                        </div>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* Delete Confirmation Modal */}
      <ConfirmModal
        isOpen={!!deleteConfirm}
        title="Delete Site"
        message={`Are you sure you want to delete "${deleteConfirm?.name}"? This will permanently remove the site, its files, and all associated data.`}
        confirmLabel="Delete Site"
        confirmVariant="danger"
        isLoading={deleting}
        onConfirm={handleDelete}
        onCancel={() => setDeleteConfirm(null)}
      />
    </div>
  )
}
