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
} from 'lucide-react'
import { api } from '@/lib/api'
import { formatDate, getStatusBgColor } from '@/lib/utils'
import type { Site } from '@/types'

export function SitesPage() {
  const [sites, setSites] = useState<Site[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [search, setSearch] = useState('')
  const [openMenu, setOpenMenu] = useState<string | null>(null)

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

  async function handleDelete(id: string) {
    if (!confirm('Are you sure you want to delete this site?')) return
    try {
      await api.deleteSite(id)
      fetchSites()
    } catch (error) {
      console.error('Failed to delete site:', error)
    }
    setOpenMenu(null)
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
        <div className="bg-card border border-white/[0.06] rounded-2xl p-16 text-center">
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
        <div className="bg-card border border-white/[0.06] rounded-2xl overflow-hidden">
          <div className="overflow-x-auto">
            <table className="w-full">
              <thead>
                <tr className="border-b border-white/[0.06]">
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
              <tbody className="divide-y divide-white/[0.04]">
                {filteredSites.map((site) => (
                  <tr key={site.id} className="hover:bg-white/[0.02] transition-colors group">
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
                          className="p-2 text-muted-foreground hover:text-foreground hover:bg-white/[0.05] rounded-lg transition-all"
                          title="Visit site"
                        >
                          <ExternalLink className="w-4 h-4" />
                        </a>
                        <div className="relative">
                          <button
                            onClick={() =>
                              setOpenMenu(openMenu === site.id ? null : site.id)
                            }
                            className="p-2 text-muted-foreground hover:text-foreground hover:bg-white/[0.05] rounded-lg transition-all"
                          >
                            <MoreVertical className="w-4 h-4" />
                          </button>
                          {openMenu === site.id && (
                            <>
                              <div
                                className="fixed inset-0 z-10"
                                onClick={() => setOpenMenu(null)}
                              />
                              <div className="absolute right-0 top-full mt-2 w-48 bg-card border border-white/[0.1] rounded-xl shadow-2xl shadow-black/50 z-20 overflow-hidden animate-fade-in">
                                <div className="py-1">
                                  {site.status === 'active' ? (
                                    <button
                                      onClick={() => handleSuspend(site.id)}
                                      className="flex items-center gap-3 w-full px-4 py-2.5 text-sm text-left hover:bg-white/[0.05] transition-colors"
                                    >
                                      <PauseCircle className="w-4 h-4 text-amber-400" />
                                      Suspend
                                    </button>
                                  ) : (
                                    <button
                                      onClick={() => handleUnsuspend(site.id)}
                                      className="flex items-center gap-3 w-full px-4 py-2.5 text-sm text-left hover:bg-white/[0.05] transition-colors"
                                    >
                                      <PlayCircle className="w-4 h-4 text-emerald-400" />
                                      Activate
                                    </button>
                                  )}
                                  {site.worker_mode && (
                                    <button
                                      onClick={() => handleRestartWorkers(site.id)}
                                      className="flex items-center gap-3 w-full px-4 py-2.5 text-sm text-left hover:bg-white/[0.05] transition-colors"
                                    >
                                      <RefreshCw className="w-4 h-4 text-blue-400" />
                                      Restart Workers
                                    </button>
                                  )}
                                  <div className="my-1 border-t border-white/[0.06]" />
                                  <button
                                    onClick={() => handleDelete(site.id)}
                                    className="flex items-center gap-3 w-full px-4 py-2.5 text-sm text-left text-red-400 hover:bg-red-500/10 transition-colors"
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
    </div>
  )
}
