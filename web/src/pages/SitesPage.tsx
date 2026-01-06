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
    primary: 'bg-emerald-500 hover:bg-emerald-600 text-white',
  }

  return (
    <div className="fixed inset-0 bg-black/60 backdrop-blur-sm flex items-center justify-center z-50 p-4">
      <div className="bg-card border border-border rounded-2xl w-full max-w-md shadow-2xl animate-fade-in">
        <div className="p-6">
          <div className="flex items-start gap-4">
            <div className={cn(
              "w-12 h-12 rounded-xl flex items-center justify-center flex-shrink-0",
              confirmVariant === 'danger' && "bg-red-500/10",
              confirmVariant === 'warning' && "bg-amber-500/10",
              confirmVariant === 'primary' && "bg-emerald-500/10",
            )}>
              <AlertCircle className={cn(
                "w-6 h-6",
                confirmVariant === 'danger' && "text-red-500",
                confirmVariant === 'warning' && "text-amber-500",
                confirmVariant === 'primary' && "text-emerald-500",
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
        <div className="w-10 h-10 border-2 border-emerald-500 border-t-transparent rounded-full animate-spin" />
      </div>
    )
  }

  return (
    <div className="space-y-6 animate-fade-in">
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
          className="flex items-center justify-center gap-2 px-5 py-2.5 bg-gradient-to-r from-emerald-500 to-teal-600 hover:from-emerald-600 hover:to-teal-700 text-white font-medium rounded-xl transition-all duration-200 shadow-lg shadow-emerald-500/20 btn-lift"
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
          className="w-full pl-12 pr-4 py-3 bg-card border border-white/[0.06] rounded-xl focus:outline-none focus:ring-2 focus:ring-emerald-500/50 focus:border-emerald-500/50 transition-all placeholder:text-muted-foreground/50"
        />
      </div>

      {/* Sites List */}
      {filteredSites.length === 0 ? (
        <div className="bg-card border border-border rounded-2xl p-16 text-center card-shadow">
          <div className="w-20 h-20 rounded-2xl bg-emerald-500/10 flex items-center justify-center mx-auto mb-6">
            {sites.length === 0 ? (
              <FolderOpen className="w-10 h-10 text-emerald-400/50" />
            ) : (
              <Search className="w-10 h-10 text-muted-foreground/50" />
            )}
          </div>
          <h3 className="font-semibold text-xl mb-2">
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
              className="inline-flex items-center gap-2 px-5 py-2.5 bg-gradient-to-r from-emerald-500 to-teal-600 hover:from-emerald-600 hover:to-teal-700 text-white font-medium rounded-xl transition-all duration-200 shadow-lg shadow-emerald-500/20 btn-lift"
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
                <tr className="border-b border-border bg-secondary/30">
                  <th className="text-left px-6 py-4 text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                    Site
                  </th>
                  <th className="text-left px-6 py-4 text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                    PHP
                  </th>
                  <th className="text-left px-6 py-4 text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                    Status
                  </th>
                  <th className="text-left px-6 py-4 text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                    Created
                  </th>
                  <th className="text-right px-6 py-4 text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                    Actions
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {filteredSites.map((site) => (
                  <tr key={site.id} className="hover:bg-secondary/30 transition-colors group">
                    <td className="px-6 py-4">
                      <Link
                        to={`/sites/${site.id}`}
                        className="flex items-center gap-4"
                      >
                        <div className="w-11 h-11 rounded-xl bg-gradient-to-br from-emerald-500/20 to-teal-500/10 flex items-center justify-center border border-emerald-500/20 group-hover:border-emerald-500/40 transition-colors">
                          <Globe className="w-5 h-5 text-emerald-400" />
                        </div>
                        <div>
                          <p className="font-medium group-hover:text-emerald-400 transition-colors">
                            {site.name}
                          </p>
                          <p className="text-sm text-muted-foreground">
                            {site.domain}
                          </p>
                        </div>
                      </Link>
                    </td>
                    <td className="px-6 py-4">
                      <span className="text-sm font-mono bg-white/[0.05] px-2.5 py-1 rounded-lg">
                        PHP {site.php_version}
                      </span>
                    </td>
                    <td className="px-6 py-4">
                      <span
                        className={`text-xs px-2.5 py-1 rounded-full border font-medium ${getStatusBgColor(
                          site.status
                        )}`}
                      >
                        {site.status}
                      </span>
                    </td>
                    <td className="px-6 py-4 text-sm text-muted-foreground">
                      {formatDate(site.created_at)}
                    </td>
                    <td className="px-6 py-4">
                      <div className="flex items-center justify-end gap-2">
                        <a
                          href={`https://${site.domain}`}
                          target="_blank"
                          rel="noopener noreferrer"
                          className="p-2 text-muted-foreground hover:text-foreground hover:bg-secondary rounded-lg transition-all"
                          title="Visit site"
                        >
                          <ExternalLink className="w-4 h-4" />
                        </a>
                        <div className="relative">
                          <button
                            onClick={() =>
                              setOpenMenu(openMenu === site.id ? null : site.id)
                            }
                            className="p-2 text-muted-foreground hover:text-foreground hover:bg-secondary rounded-lg transition-all"
                          >
                            <MoreVertical className="w-4 h-4" />
                          </button>
                          {openMenu === site.id && (
                            <>
                              <div
                                className="fixed inset-0 z-10"
                                onClick={() => setOpenMenu(null)}
                              />
                              <div className="absolute right-0 top-full mt-2 w-48 bg-card border border-border rounded-xl shadow-xl z-20 overflow-hidden animate-fade-in">
                                <div className="py-1">
                                  {site.status === 'active' ? (
                                    <button
                                      onClick={() => handleSuspend(site.id)}
                                      className="flex items-center gap-3 w-full px-4 py-2.5 text-sm text-left hover:bg-secondary transition-colors"
                                    >
                                      <PauseCircle className="w-4 h-4 text-amber-500" />
                                      Suspend
                                    </button>
                                  ) : (
                                    <button
                                      onClick={() => handleUnsuspend(site.id)}
                                      className="flex items-center gap-3 w-full px-4 py-2.5 text-sm text-left hover:bg-secondary transition-colors"
                                    >
                                      <PlayCircle className="w-4 h-4 text-emerald-500" />
                                      Activate
                                    </button>
                                  )}
                                  {site.worker_mode && (
                                    <button
                                      onClick={() => handleRestartWorkers(site.id)}
                                      className="flex items-center gap-3 w-full px-4 py-2.5 text-sm text-left hover:bg-secondary transition-colors"
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
                                    className="flex items-center gap-3 w-full px-4 py-2.5 text-sm text-left text-red-500 hover:bg-red-500/10 transition-colors"
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
