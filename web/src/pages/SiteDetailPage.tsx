import { useEffect, useState, useCallback } from 'react'
import { useParams, Link, useNavigate } from 'react-router-dom'
import {
  ArrowLeft,
  Globe,
  ExternalLink,
  Trash2,
  Save,
  Loader2,
  FolderOpen,
  Clock,
  X,
  Plus,
  AlertCircle,
} from 'lucide-react'
import { api } from '@/lib/api'
import { formatDate, getStatusBgColor } from '@/lib/utils'
import type { Site, PHPInstance } from '@/types'

function normalizeDomain(domain: string): string {
  let d = domain.trim().toLowerCase()
  // Strip http(s)://
  d = d.replace(/^https?:\/\//, '')
  // Strip path, query, port
  d = d.split('/')[0].split(':')[0].split('?')[0]
  return d
}

export function SiteDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [site, setSite] = useState<Site | null>(null)
  const [phpVersions, setPHPVersions] = useState<PHPInstance[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [isSaving, setIsSaving] = useState(false)
  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')

  const [form, setForm] = useState({
    name: '',
    domain: '',
    php_version: '',
    public_path: '',
  })
  const [aliases, setAliases] = useState<string[]>([])
  const [aliasInput, setAliasInput] = useState('')
  const [aliasError, setAliasError] = useState('')

  const addAlias = useCallback(() => {
    setAliasError('')
    const normalized = normalizeDomain(aliasInput)
    if (!normalized) {
      setAliasInput('')
      return
    }
    const normalizedPrimary = normalizeDomain(form.domain)
    if (normalized === normalizedPrimary) {
      setAliasError('Alias cannot be the same as the primary domain')
      return
    }
    if (aliases.includes(normalized)) {
      setAliasError('This domain is already in the list')
      return
    }
    setAliases((prev) => [...prev, normalized])
    setAliasInput('')
  }, [aliasInput, form.domain, aliases])

  const removeAlias = useCallback((alias: string) => {
    setAliases((prev) => prev.filter((a) => a !== alias))
  }, [])

  useEffect(() => {
    async function fetchData() {
      if (!id) return
      try {
        const [siteData, phpData] = await Promise.all([
          api.getSite(id),
          api.getPHPInstances(),
        ])
        setSite(siteData)
        setPHPVersions(phpData.instances || [])
        setForm({
          name: siteData.name,
          domain: siteData.domain,
          php_version: siteData.php_version,
          public_path: siteData.public_path,
        })
        setAliases(siteData.aliases || [])
      } catch (error) {
        console.error('Failed to fetch site:', error)
      } finally {
        setIsLoading(false)
      }
    }
    fetchData()
  }, [id])

  const handleSave = async () => {
    if (!id) return
    setError('')
    setSuccess('')
    setIsSaving(true)

    try {
      const updated = await api.updateSite(id, {
        name: form.name,
        domain: form.domain,
        aliases: aliases,
        php_version: form.php_version,
        public_path: form.public_path,
      })
      setSite(updated)
      setAliases(updated.aliases || [])
      setSuccess('Site updated successfully')
      setTimeout(() => setSuccess(''), 3000)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update site')
    } finally {
      setIsSaving(false)
    }
  }

  const handleDelete = async () => {
    if (!id) return
    if (!confirm('Are you sure you want to delete this site? This action cannot be undone.')) return

    try {
      await api.deleteSite(id)
      navigate('/sites')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete site')
    }
  }

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="w-8 h-8 border-2 border-emerald-500 border-t-transparent rounded-full animate-spin" />
      </div>
    )
  }

  if (!site) {
    return (
      <div className="text-center py-12">
        <h2 className="text-xl font-semibold mb-2">Site not found</h2>
        <Link to="/sites" className="text-emerald-400 hover:text-emerald-300">
          Back to sites
        </Link>
      </div>
    )
  }

  return (
    <div className="max-w-4xl mx-auto animate-fade-in">
      {/* Header */}
      <div className="mb-6">
        <Link
          to="/sites"
          className="inline-flex items-center gap-2 text-muted-foreground hover:text-foreground mb-4"
        >
          <ArrowLeft className="w-4 h-4" />
          Back to Sites
        </Link>

        <div className="flex items-start justify-between">
          <div className="flex items-center gap-4">
            <div className="w-14 h-14 rounded-xl bg-gradient-to-br from-emerald-500/20 to-emerald-600/20 flex items-center justify-center border border-emerald-500/20">
              <Globe className="w-7 h-7 text-emerald-400" />
            </div>
            <div>
              <h1 className="text-2xl font-bold">{site.name}</h1>
              <div className="flex items-center gap-3 mt-1">
                <a
                  href={`https://${site.domain}`}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="text-muted-foreground hover:text-emerald-400 flex items-center gap-1"
                >
                  {site.domain}
                  <ExternalLink className="w-3 h-3" />
                </a>
                <span
                  className={`text-xs px-2 py-0.5 rounded-full border ${getStatusBgColor(
                    site.status
                  )}`}
                >
                  {site.status}
                </span>
              </div>
            </div>
          </div>

          <button
            onClick={handleDelete}
            className="flex items-center gap-2 px-3 py-2 text-sm text-red-400 bg-red-500/10 hover:bg-red-500/20 rounded-lg transition-colors"
          >
            <Trash2 className="w-4 h-4" />
            Delete
          </button>
        </div>
      </div>

      {/* Messages */}
      {error && (
        <div className="mb-6 bg-red-500/10 border border-red-500/20 text-red-400 px-4 py-3 rounded-lg text-sm">
          {error}
        </div>
      )}
      {success && (
        <div className="mb-6 bg-emerald-500/10 border border-emerald-500/20 text-emerald-400 px-4 py-3 rounded-lg text-sm">
          {success}
        </div>
      )}

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* Main Form */}
        <div className="lg:col-span-2 space-y-6">
          {/* Basic Settings */}
          <div className="bg-card border border-border rounded-xl p-6 space-y-5">
            <h2 className="font-semibold pb-4 border-b border-border">
              Site Settings
            </h2>

            <div className="space-y-2">
              <label className="block text-sm font-medium">Site Name</label>
              <input
                type="text"
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                className="w-full px-4 py-2.5 bg-secondary border border-border rounded-lg focus:outline-none focus:ring-2 focus:ring-emerald-500/50 focus:border-emerald-500 transition-colors"
              />
            </div>

            <div className="space-y-2">
              <label className="block text-sm font-medium">Domain</label>
              <input
                type="text"
                value={form.domain}
                onChange={(e) => setForm({ ...form, domain: e.target.value })}
                className="w-full px-4 py-2.5 bg-secondary border border-border rounded-lg focus:outline-none focus:ring-2 focus:ring-emerald-500/50 focus:border-emerald-500 transition-colors"
              />
              <p className="text-xs text-muted-foreground">
                Primary domain. All additional domains will permanently redirect to this.
              </p>
            </div>

            <div className="space-y-2">
              <label className="block text-sm font-medium">
                Additional Domains <span className="text-muted-foreground font-normal">(optional)</span>
              </label>
              
              {/* Alias chips */}
              {aliases.length > 0 && (
                <div className="flex flex-wrap gap-2 mb-2">
                  {aliases.map((alias) => (
                    <span
                      key={alias}
                      className="inline-flex items-center gap-1.5 px-3 py-1.5 bg-secondary border border-border rounded-lg text-sm group"
                    >
                      <Globe className="w-3.5 h-3.5 text-muted-foreground" />
                      {alias}
                      <button
                        type="button"
                        onClick={() => removeAlias(alias)}
                        className="ml-1 p-0.5 rounded hover:bg-red-500/20 hover:text-red-400 transition-colors"
                      >
                        <X className="w-3.5 h-3.5" />
                      </button>
                    </span>
                  ))}
                </div>
              )}
              
              {/* Add alias input */}
              <div className="flex gap-2">
                <input
                  type="text"
                  value={aliasInput}
                  onChange={(e) => {
                    setAliasInput(e.target.value)
                    setAliasError('')
                  }}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') {
                      e.preventDefault()
                      addAlias()
                    }
                  }}
                  className="flex-1 px-4 py-2.5 bg-secondary border border-border rounded-lg focus:outline-none focus:ring-2 focus:ring-emerald-500/50 focus:border-emerald-500 transition-colors"
                  placeholder="www.example.com"
                />
                <button
                  type="button"
                  onClick={addAlias}
                  className="px-4 py-2.5 bg-secondary border border-border rounded-lg hover:bg-secondary/80 transition-colors flex items-center gap-2 text-sm font-medium"
                >
                  <Plus className="w-4 h-4" />
                  Add
                </button>
              </div>
              
              {aliasError && (
                <p className="text-xs text-red-400 flex items-center gap-1">
                  <AlertCircle className="w-3 h-3" />
                  {aliasError}
                </p>
              )}
              
              <p className="text-xs text-muted-foreground">
                These domains will permanently redirect to the primary domain.
              </p>
            </div>

            <div className="space-y-2">
              <label className="block text-sm font-medium">PHP Version</label>
              <select
                value={form.php_version}
                onChange={(e) =>
                  setForm({ ...form, php_version: e.target.value })
                }
                className="w-full px-4 py-2.5 bg-secondary border border-border rounded-lg focus:outline-none focus:ring-2 focus:ring-emerald-500/50 focus:border-emerald-500 transition-colors"
              >
                {phpVersions.map((php) => (
                  <option key={php.version} value={php.version}>
                    PHP {php.version}
                  </option>
                ))}
              </select>
            </div>

            <div className="space-y-2">
              <label className="block text-sm font-medium">Public Directory</label>
              <input
                type="text"
                value={form.public_path}
                onChange={(e) =>
                  setForm({ ...form, public_path: e.target.value })
                }
                className="w-full px-4 py-2.5 bg-secondary border border-border rounded-lg focus:outline-none focus:ring-2 focus:ring-emerald-500/50 focus:border-emerald-500 transition-colors"
              />
            </div>
          </div>

          {/* Save Button */}
          <button
            onClick={handleSave}
            disabled={isSaving}
            className="w-full py-3 px-4 bg-gradient-to-r from-emerald-500 to-emerald-600 hover:from-emerald-600 hover:to-emerald-700 text-white font-medium rounded-lg transition-all duration-200 disabled:opacity-50 disabled:cursor-not-allowed flex items-center justify-center gap-2"
          >
            {isSaving ? (
              <>
                <Loader2 className="w-4 h-4 animate-spin" />
                Saving...
              </>
            ) : (
              <>
                <Save className="w-4 h-4" />
                Save Changes
              </>
            )}
          </button>
        </div>

        {/* Sidebar */}
        <div className="space-y-6">
          {/* Info Card */}
          <div className="bg-card border border-border rounded-xl p-5 space-y-4">
            <h3 className="font-semibold">Site Information</h3>

            <div className="space-y-3 text-sm">
              <div className="flex items-center gap-3">
                <FolderOpen className="w-4 h-4 text-muted-foreground" />
                <div>
                  <p className="text-muted-foreground">Root Path</p>
                  <p className="font-mono text-xs">{site.root_path}</p>
                </div>
              </div>

              <div className="flex items-center gap-3">
                <Clock className="w-4 h-4 text-muted-foreground" />
                <div>
                  <p className="text-muted-foreground">Created</p>
                  <p>{formatDate(site.created_at)}</p>
                </div>
              </div>

              <div className="flex items-center gap-3">
                <Clock className="w-4 h-4 text-muted-foreground" />
                <div>
                  <p className="text-muted-foreground">Last Updated</p>
                  <p>{formatDate(site.updated_at)}</p>
                </div>
              </div>
            </div>
          </div>

          {/* Quick Links */}
          <div className="bg-card border border-border rounded-xl p-5 space-y-4">
            <h3 className="font-semibold">Quick Actions</h3>

            <div className="space-y-2">
              <a
                href={`https://${site.domain}`}
                target="_blank"
                rel="noopener noreferrer"
                className="flex items-center gap-2 px-3 py-2 bg-secondary hover:bg-secondary/80 rounded-lg transition-colors text-sm"
              >
                <ExternalLink className="w-4 h-4" />
                Visit Site
              </a>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

