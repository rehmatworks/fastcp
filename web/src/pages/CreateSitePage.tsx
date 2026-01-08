import { useState, useEffect, useCallback } from 'react'
import { useNavigate, Link } from 'react-router-dom'
import { ArrowLeft, Loader2, Globe, FileCode, CircleDot, Database, AlertCircle, X, Plus } from 'lucide-react'
import { api, DatabaseStatus } from '@/lib/api'
import { cn } from '@/lib/utils'
import type { PHPInstance } from '@/types'

const APP_TYPES = [
  {
    id: 'blank',
    name: 'Blank Site',
    description: 'Empty PHP site with default landing page',
    icon: FileCode,
  },
  {
    id: 'wordpress',
    name: 'WordPress',
    description: 'Latest WordPress with auto-configured database',
    icon: CircleDot,
  },
]

function normalizeDomain(domain: string): string {
  let d = domain.trim().toLowerCase()
  // Strip http(s)://
  d = d.replace(/^https?:\/\//, '')
  // Strip path, query, port
  d = d.split('/')[0].split(':')[0].split('?')[0]
  return d
}

export function CreateSitePage() {
  const navigate = useNavigate()
  const [isLoading, setIsLoading] = useState(false)
  const [phpVersions, setPHPVersions] = useState<PHPInstance[]>([])
  const [error, setError] = useState('')
  const [mysqlStatus, setMysqlStatus] = useState<DatabaseStatus | null>(null)

  const [form, setForm] = useState({
    name: '',
    domain: '',
    php_version: '8.4',
    public_path: 'public',
    app_type: 'blank',
  })
  const [aliases, setAliases] = useState<string[]>([])
  const [aliasInput, setAliasInput] = useState('')
  const [aliasError, setAliasError] = useState('')

  const isMySQLReady = mysqlStatus?.mysql?.installed && mysqlStatus?.mysql?.running

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
      try {
        // Fetch PHP versions
        const phpData = await api.getPHPInstances()
        setPHPVersions(phpData.instances || [])
        if (phpData.instances?.length > 0) {
          setForm((f) => ({ ...f, php_version: phpData.instances[0].version }))
        }

        // Fetch MySQL status
        const dbStatus = await api.getDatabaseStatus()
        setMysqlStatus(dbStatus)
      } catch (error) {
        console.error('Failed to fetch data:', error)
      }
    }
    fetchData()
  }, [])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setIsLoading(true)

    try {
      const site = await api.createSite({
        name: form.name || form.domain,
        domain: form.domain,
        aliases: aliases,
        php_version: form.php_version,
        public_path: form.app_type === 'wordpress' ? '' : form.public_path, // WordPress uses root
        app_type: form.app_type as 'blank' | 'wordpress',
      })
      navigate(`/sites/${site.id}`)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create site')
    } finally {
      setIsLoading(false)
    }
  }

  return (
    <div className="max-w-2xl mx-auto animate-fade-in">
      {/* Header */}
      <div className="mb-6">
        <Link
          to="/sites"
          className="inline-flex items-center gap-2 text-muted-foreground hover:text-foreground mb-4"
        >
          <ArrowLeft className="w-4 h-4" />
          Back to Sites
        </Link>
        <h1 className="text-2xl font-bold">Create New Site</h1>
        <p className="text-muted-foreground">
          Deploy a new PHP website or application
        </p>
      </div>

      {/* Form */}
      <form onSubmit={handleSubmit} className="space-y-6">
        {error && (
          <div className="bg-red-500/10 border border-red-500/20 text-red-400 px-4 py-3 rounded-lg text-sm">
            {error}
          </div>
        )}

        {/* App Type Selection */}
        <div className="bg-card border border-border rounded-xl p-6 space-y-4">
          <h2 className="font-semibold">Choose Application Type</h2>
          <div className="grid grid-cols-2 gap-4">
            {APP_TYPES.map((appType) => {
              const Icon = appType.icon
              const isSelected = form.app_type === appType.id
              const isWordPress = appType.id === 'wordpress'
              const isDisabled = isWordPress && !isMySQLReady

              return (
                <button
                  key={appType.id}
                  type="button"
                  onClick={() => {
                    if (!isDisabled) {
                      setForm({ ...form, app_type: appType.id })
                    }
                  }}
                  disabled={isDisabled}
                  className={cn(
                    "relative p-4 rounded-xl border-2 text-left transition-all",
                    isDisabled
                      ? "border-border bg-secondary/30 opacity-60 cursor-not-allowed"
                      : isSelected
                        ? "border-emerald-500 bg-emerald-500/10"
                        : "border-border hover:border-muted-foreground/50 bg-secondary/50"
                  )}
                >
                  <div className="flex items-start gap-3">
                    <div className={cn(
                      "w-10 h-10 rounded-lg flex items-center justify-center",
                      isDisabled
                        ? "bg-muted/50"
                        : isSelected
                          ? "bg-emerald-500/20"
                          : "bg-muted"
                    )}>
                      <Icon className={cn(
                        "w-5 h-5",
                        isDisabled
                          ? "text-muted-foreground/50"
                          : isSelected
                            ? "text-emerald-400"
                            : "text-muted-foreground"
                      )} />
                    </div>
                    <div className="flex-1">
                      <h3 className={cn(
                        "font-medium",
                        isDisabled
                          ? "text-muted-foreground/70"
                          : isSelected && "text-emerald-400"
                      )}>
                        {appType.name}
                      </h3>
                      <p className="text-xs text-muted-foreground mt-0.5">
                        {isDisabled ? 'Requires MySQL database' : appType.description}
                      </p>
                    </div>
                  </div>
                  {isSelected && !isDisabled && (
                    <div className="absolute top-2 right-2 w-2 h-2 rounded-full bg-emerald-500" />
                  )}
                </button>
              )
            })}
          </div>

          {/* MySQL not ready warning */}
          {!isMySQLReady && (
            <div className="bg-amber-500/10 border border-amber-500/20 text-amber-400 px-4 py-3 rounded-lg text-sm flex items-start gap-3">
              <AlertCircle className="w-5 h-5 flex-shrink-0 mt-0.5" />
              <div>
                <strong>MySQL Required for WordPress:</strong> To create WordPress sites, you need to set up MySQL first.
                <Link
                  to="/databases"
                  className="ml-2 inline-flex items-center gap-1 text-amber-300 hover:text-amber-200 underline underline-offset-2"
                >
                  <Database className="w-4 h-4" />
                  Go to Databases
                </Link>
              </div>
            </div>
          )}

          {form.app_type === 'wordpress' && isMySQLReady && (
            <div className="bg-blue-500/10 border border-blue-500/20 text-blue-400 px-4 py-3 rounded-lg text-sm">
              <strong>WordPress:</strong> A database will be automatically created and configured. You'll complete the WordPress installation in your browser after creation.
            </div>
          )}
        </div>

        <div className="bg-card border border-border rounded-xl p-6 space-y-5">
          <div className="flex items-center gap-3 pb-4 border-b border-border">
            <div className="w-10 h-10 rounded-lg bg-gradient-to-br from-emerald-500/20 to-emerald-600/20 flex items-center justify-center border border-emerald-500/20">
              <Globe className="w-5 h-5 text-emerald-400" />
            </div>
            <div>
              <h2 className="font-semibold">Site Details</h2>
              <p className="text-sm text-muted-foreground">Basic site information</p>
            </div>
          </div>

          <div className="space-y-2">
            <label htmlFor="name" className="block text-sm font-medium">
              Site Name
            </label>
            <input
              id="name"
              type="text"
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
              className="w-full px-4 py-2.5 bg-secondary border border-border rounded-lg focus:outline-none focus:ring-2 focus:ring-emerald-500/50 focus:border-emerald-500 transition-colors"
              placeholder="My Awesome Site"
            />
            <p className="text-xs text-muted-foreground">
              Optional. Defaults to domain name.
            </p>
          </div>

          <div className="space-y-2">
            <label htmlFor="domain" className="block text-sm font-medium">
              Domain <span className="text-red-400">*</span>
            </label>
            <input
              id="domain"
              type="text"
              value={form.domain}
              onChange={(e) => setForm({ ...form, domain: e.target.value })}
              className="w-full px-4 py-2.5 bg-secondary border border-border rounded-lg focus:outline-none focus:ring-2 focus:ring-emerald-500/50 focus:border-emerald-500 transition-colors"
              placeholder="example.com"
              required
            />
            <p className="text-xs text-muted-foreground">
              This is the <strong>primary</strong> domain. Any additional domains will permanently redirect to it.
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
            <label htmlFor="php_version" className="block text-sm font-medium">
              PHP Version
            </label>
            <select
              id="php_version"
              value={form.php_version}
              onChange={(e) => setForm({ ...form, php_version: e.target.value })}
              className="w-full px-4 py-2.5 bg-secondary border border-border rounded-lg focus:outline-none focus:ring-2 focus:ring-emerald-500/50 focus:border-emerald-500 transition-colors"
            >
              {phpVersions.map((php) => (
                <option key={php.version} value={php.version}>
                  PHP {php.version} ({php.status})
                </option>
              ))}
            </select>
          </div>

          <div className="space-y-2">
            <label htmlFor="public_path" className="block text-sm font-medium">
              Public Directory
            </label>
            <input
              id="public_path"
              type="text"
              value={form.public_path}
              onChange={(e) => setForm({ ...form, public_path: e.target.value })}
              className="w-full px-4 py-2.5 bg-secondary border border-border rounded-lg focus:outline-none focus:ring-2 focus:ring-emerald-500/50 focus:border-emerald-500 transition-colors"
              placeholder="public"
            />
            <p className="text-xs text-muted-foreground">
              The publicly accessible directory (e.g., "public", "web", "html")
            </p>
          </div>
        </div>

        {/* Submit */}
        <div className="flex items-center gap-4">
          <button
            type="submit"
            disabled={isLoading || !form.domain}
            className="flex-1 py-3 px-4 bg-gradient-to-r from-emerald-500 to-emerald-600 hover:from-emerald-600 hover:to-emerald-700 text-white font-medium rounded-lg transition-all duration-200 disabled:opacity-50 disabled:cursor-not-allowed flex items-center justify-center gap-2"
          >
            {isLoading ? (
              <>
                <Loader2 className="w-4 h-4 animate-spin" />
                Creating Site...
              </>
            ) : (
              'Create Site'
            )}
          </button>
          <Link
            to="/sites"
            className="px-6 py-3 text-muted-foreground hover:text-foreground transition-colors"
          >
            Cancel
          </Link>
        </div>
      </form>
    </div>
  )
}

