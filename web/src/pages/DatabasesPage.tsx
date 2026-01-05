import { useEffect, useState, useRef, useCallback } from 'react'
import {
  Database,
  Plus,
  Trash2,
  Copy,
  Check,
  Server,
  AlertTriangle,
  Loader2,
  Key,
} from 'lucide-react'
import { api, MySQLInstallStatus } from '@/lib/api'
import { getStatusBgColor, cn } from '@/lib/utils'
import { useAuth } from '@/contexts/AuthContext'

interface DatabaseItem {
  id: string
  user_id: string
  site_id?: string
  name: string
  username: string
  password?: string
  host: string
  port: number
  created_at: string
}

interface DatabaseStatus {
  installed: boolean
  running: boolean
  version?: string
  database_count: number
}

export function DatabasesPage() {
  const { user } = useAuth()
  const [databases, setDatabases] = useState<DatabaseItem[]>([])
  const [status, setStatus] = useState<DatabaseStatus | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [showCreateModal, setShowCreateModal] = useState(false)
  const [showResetPasswordModal, setShowResetPasswordModal] = useState<DatabaseItem | null>(null)
  const [creating, setCreating] = useState(false)
  const [resettingPassword, setResettingPassword] = useState(false)
  const [installing, setInstalling] = useState(false)
  const [installStatus, setInstallStatus] = useState<MySQLInstallStatus | null>(null)
  const [error, setError] = useState('')
  const [copiedId, setCopiedId] = useState<string | null>(null)
  const [newDatabase, setNewDatabase] = useState<DatabaseItem | null>(null)
  const [newPassword, setNewPassword] = useState('')
  const [showNewPassword, setShowNewPassword] = useState<{ db: DatabaseItem; password: string } | null>(null)
  const pollIntervalRef = useRef<NodeJS.Timeout | null>(null)

  const [form, setForm] = useState({
    name: '',
    username: '',
    password: '',
  })

  // Poll for installation status
  const pollInstallStatus = useCallback(async () => {
    try {
      const status = await api.getMySQLInstallStatus()
      setInstallStatus(status)

      if (!status.in_progress) {
        // Installation finished, stop polling
        if (pollIntervalRef.current) {
          clearInterval(pollIntervalRef.current)
          pollIntervalRef.current = null
        }
        setInstalling(false)

        if (status.success) {
          // Refresh data to show MySQL is installed
          fetchData()
        }
      }
    } catch (error) {
      console.error('Failed to get install status:', error)
    }
  }, [])

  // Cleanup interval on unmount
  useEffect(() => {
    return () => {
      if (pollIntervalRef.current) {
        clearInterval(pollIntervalRef.current)
      }
    }
  }, [])

  useEffect(() => {
    fetchData()
  }, [])

  async function fetchData() {
    try {
      const [dbData, statusData] = await Promise.all([
        api.getDatabases(),
        api.getDatabaseStatus(),
      ])
      setDatabases(dbData.databases || [])
      setStatus(statusData)
    } catch (error) {
      console.error('Failed to fetch databases:', error)
    } finally {
      setIsLoading(false)
    }
  }

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault()
    setError('')
    setCreating(true)

    try {
      const created = await api.createDatabase({
        name: form.name,
        username: form.username || form.name,
        password: form.password || undefined,
      })
      setNewDatabase(created)
      setShowCreateModal(false)
      setForm({ name: '', username: '', password: '' })
      fetchData()
    } catch (err: any) {
      setError(err.message || 'Failed to create database')
    } finally {
      setCreating(false)
    }
  }

  async function handleDelete(db: DatabaseItem) {
    if (!confirm(`Delete database "${db.name}"? This cannot be undone.`)) return

    try {
      await api.deleteDatabase(db.id)
      fetchData()
    } catch (error) {
      console.error('Failed to delete database:', error)
    }
  }

  async function handleInstall() {
    if (!confirm('Install MySQL server? This will download and configure MySQL. This may take a few minutes.')) return

    setInstalling(true)
    setInstallStatus({ in_progress: true, success: false, message: 'Starting installation...' })

    try {
      const response = await api.installMySQL()

      // If it's already installed (adopted), we're done
      if (response.status === 'completed') {
        setInstalling(false)
        setInstallStatus({ in_progress: false, success: true, message: response.message })
        fetchData()
        return
      }

      // Otherwise, start polling for status
      pollIntervalRef.current = setInterval(pollInstallStatus, 2000)
    } catch (error: any) {
      setInstalling(false)
      setInstallStatus({ in_progress: false, success: false, error: error.message || 'Unknown error' })
    }
  }

  async function handleResetPassword(e: React.FormEvent) {
    e.preventDefault()
    if (!showResetPasswordModal) return

    setError('')
    setResettingPassword(true)

    try {
      await api.resetDatabasePassword(showResetPasswordModal.id, newPassword)
      setShowNewPassword({ db: showResetPasswordModal, password: newPassword })
      setShowResetPasswordModal(null)
      setNewPassword('')
    } catch (err: any) {
      setError(err.message || 'Failed to reset password')
    } finally {
      setResettingPassword(false)
    }
  }

  function generateRandomPassword() {
    const chars = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*'
    let password = ''
    for (let i = 0; i < 24; i++) {
      password += chars.charAt(Math.floor(Math.random() * chars.length))
    }
    setNewPassword(password)
  }

  async function copyToClipboard(text: string, id: string) {
    try {
      // Try the modern clipboard API first
      if (navigator.clipboard && navigator.clipboard.writeText) {
        await navigator.clipboard.writeText(text)
      } else {
        // Fallback for older browsers or non-HTTPS contexts
        const textArea = document.createElement('textarea')
        textArea.value = text
        textArea.style.position = 'fixed'
        textArea.style.left = '-999999px'
        textArea.style.top = '-999999px'
        document.body.appendChild(textArea)
        textArea.focus()
        textArea.select()
        document.execCommand('copy')
        document.body.removeChild(textArea)
      }
      setCopiedId(id)
      setTimeout(() => setCopiedId(null), 2000)
    } catch (err) {
      console.error('Failed to copy:', err)
      // Still show visual feedback even if copy failed
      alert('Failed to copy to clipboard. Please copy manually: ' + text)
    }
  }

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loader2 className="w-8 h-8 animate-spin text-muted-foreground" />
      </div>
    )
  }

  // MySQL not installed
  if (status && !status.installed) {
    return (
      <div className="space-y-6">
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-2xl font-bold">Databases</h1>
            <p className="text-muted-foreground mt-1">Manage MySQL databases</p>
          </div>
        </div>

        <div className="bg-card border border-border rounded-xl p-12 text-center">
          <Server className="w-16 h-16 mx-auto mb-4 text-muted-foreground" />
          <h2 className="text-xl font-semibold mb-2">MySQL Not Installed</h2>
          <p className="text-muted-foreground mb-6 max-w-md mx-auto">
            MySQL server is not installed on this system. Click below to install and secure MySQL automatically.
          </p>

          {/* Installation progress */}
          {installing && installStatus?.in_progress && (
            <div className="mb-6 p-4 bg-blue-500/10 border border-blue-500/20 rounded-lg max-w-md mx-auto">
              <div className="flex items-center gap-3">
                <Loader2 className="w-5 h-5 animate-spin text-blue-400" />
                <div className="text-left">
                  <p className="text-blue-400 font-medium">Installing MySQL...</p>
                  <p className="text-sm text-muted-foreground">
                    {installStatus.message || 'This may take a few minutes. Please wait...'}
                  </p>
                </div>
              </div>
            </div>
          )}

          {/* Installation error */}
          {installStatus && !installStatus.in_progress && installStatus.error && (
            <div className="mb-6 p-4 bg-red-500/10 border border-red-500/20 rounded-lg max-w-md mx-auto">
              <div className="flex items-start gap-3">
                <AlertTriangle className="w-5 h-5 text-red-400 mt-0.5" />
                <div className="text-left">
                  <p className="text-red-400 font-medium">Installation Failed</p>
                  <p className="text-sm text-muted-foreground">{installStatus.error}</p>
                </div>
              </div>
            </div>
          )}

          {/* Installation success */}
          {installStatus && !installStatus.in_progress && installStatus.success && (
            <div className="mb-6 p-4 bg-emerald-500/10 border border-emerald-500/20 rounded-lg max-w-md mx-auto">
              <div className="flex items-center gap-3">
                <Check className="w-5 h-5 text-emerald-400" />
                <div className="text-left">
                  <p className="text-emerald-400 font-medium">MySQL Installed Successfully!</p>
                  <p className="text-sm text-muted-foreground">{installStatus.message}</p>
                </div>
              </div>
            </div>
          )}

          {/* Only show button when not installing */}
          {!installing && user?.role === 'admin' && (
            <button
              onClick={handleInstall}
              className="inline-flex items-center gap-2 px-6 py-3 bg-emerald-500 hover:bg-emerald-600 text-white rounded-lg transition-colors"
            >
              <Server className="w-5 h-5" />
              Install MySQL
            </button>
          )}
          {!installing && user?.role !== 'admin' && (
            <p className="text-amber-400">Contact an administrator to install MySQL.</p>
          )}
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">Databases</h1>
          <p className="text-muted-foreground mt-1">
            Manage MySQL databases for your sites
          </p>
        </div>
        <button
          onClick={() => setShowCreateModal(true)}
          className="flex items-center gap-2 px-4 py-2 bg-emerald-500 hover:bg-emerald-600 text-white rounded-lg transition-colors"
        >
          <Plus className="w-4 h-4" />
          New Database
        </button>
      </div>

      {/* Status Card */}
      {status && (
        <div className="bg-card border border-border rounded-xl p-4">
          <div className="flex items-center gap-4">
            <div className={cn(
              "w-10 h-10 rounded-full flex items-center justify-center",
              status.running ? "bg-emerald-500/20" : "bg-red-500/20"
            )}>
              <Database className={cn(
                "w-5 h-5",
                status.running ? "text-emerald-400" : "text-red-400"
              )} />
            </div>
            <div className="flex-1">
              <div className="flex items-center gap-2">
                <span className="font-medium">MySQL Server</span>
                <span className={cn(
                  "text-xs px-2 py-0.5 rounded-full",
                  getStatusBgColor(status.running ? 'running' : 'stopped')
                )}>
                  {status.running ? 'Running' : 'Stopped'}
                </span>
              </div>
              {status.version && (
                <p className="text-sm text-muted-foreground">{status.version}</p>
              )}
            </div>
            <div className="text-right">
              <p className="text-2xl font-bold">{databases.length}</p>
              <p className="text-xs text-muted-foreground">Databases</p>
            </div>
          </div>
        </div>
      )}

      {/* New Database Credentials */}
      {newDatabase && newDatabase.password && (
        <div className="bg-emerald-500/10 border border-emerald-500/20 rounded-xl p-6">
          <div className="flex items-start gap-3">
            <Check className="w-6 h-6 text-emerald-400 mt-0.5" />
            <div className="flex-1">
              <h3 className="font-semibold text-emerald-400 mb-2">Database Created Successfully!</h3>
              <p className="text-sm text-muted-foreground mb-4">
                Save these credentials - the password won't be shown again.
              </p>
              <div className="grid grid-cols-2 gap-4 bg-secondary/50 rounded-lg p-4">
                <div>
                  <p className="text-xs text-muted-foreground mb-1">Database Name</p>
                  <div className="flex items-center gap-2">
                    <code className="text-sm">{newDatabase.name}</code>
                    <button
                      onClick={() => copyToClipboard(newDatabase.name, 'name')}
                      className="text-muted-foreground hover:text-foreground"
                    >
                      {copiedId === 'name' ? <Check className="w-4 h-4 text-emerald-400" /> : <Copy className="w-4 h-4" />}
                    </button>
                  </div>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground mb-1">Username</p>
                  <div className="flex items-center gap-2">
                    <code className="text-sm">{newDatabase.username}</code>
                    <button
                      onClick={() => copyToClipboard(newDatabase.username, 'user')}
                      className="text-muted-foreground hover:text-foreground"
                    >
                      {copiedId === 'user' ? <Check className="w-4 h-4 text-emerald-400" /> : <Copy className="w-4 h-4" />}
                    </button>
                  </div>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground mb-1">Password</p>
                  <div className="flex items-center gap-2">
                    <code className="text-sm font-mono">{newDatabase.password}</code>
                    <button
                      onClick={() => copyToClipboard(newDatabase.password!, 'pass')}
                      className="text-muted-foreground hover:text-foreground"
                    >
                      {copiedId === 'pass' ? <Check className="w-4 h-4 text-emerald-400" /> : <Copy className="w-4 h-4" />}
                    </button>
                  </div>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground mb-1">Host</p>
                  <code className="text-sm">{newDatabase.host}:{newDatabase.port}</code>
                </div>
              </div>
              <button
                onClick={() => setNewDatabase(null)}
                className="mt-4 text-sm text-muted-foreground hover:text-foreground"
              >
                Dismiss
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Database List */}
      {databases.length === 0 ? (
        <div className="bg-card border border-border rounded-xl p-12 text-center">
          <Database className="w-12 h-12 mx-auto mb-4 text-muted-foreground opacity-50" />
          <h3 className="font-semibold text-lg mb-2">No Databases</h3>
          <p className="text-muted-foreground mb-4">
            Create your first database to get started
          </p>
          <button
            onClick={() => setShowCreateModal(true)}
            className="inline-flex items-center gap-2 px-4 py-2 bg-emerald-500 hover:bg-emerald-600 text-white rounded-lg transition-colors"
          >
            <Plus className="w-4 h-4" />
            Create Database
          </button>
        </div>
      ) : (
        <div className="grid gap-4">
          {databases.map((db) => (
            <div
              key={db.id}
              className="bg-card border border-border rounded-xl p-5"
            >
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-4">
                  <div className="w-10 h-10 rounded-full bg-blue-500/20 flex items-center justify-center">
                    <Database className="w-5 h-5 text-blue-400" />
                  </div>
                  <div>
                    <h3 className="font-semibold">{db.name}</h3>
                    <p className="text-sm text-muted-foreground">
                      User: {db.username} • Host: {db.host}:{db.port}
                    </p>
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  <button
                    onClick={() => {
                      setShowResetPasswordModal(db)
                      setNewPassword('')
                      setError('')
                    }}
                    className="p-2 text-muted-foreground hover:text-amber-400 hover:bg-amber-500/10 rounded-lg transition-colors"
                    title="Reset password"
                  >
                    <Key className="w-4 h-4" />
                  </button>
                  <button
                    onClick={() => handleDelete(db)}
                    className="p-2 text-muted-foreground hover:text-red-400 hover:bg-red-500/10 rounded-lg transition-colors"
                    title="Delete database"
                  >
                    <Trash2 className="w-4 h-4" />
                  </button>
                </div>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* New Password Display */}
      {showNewPassword && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
          <div className="bg-card border border-border rounded-xl w-full max-w-md p-6 m-4">
            <div className="flex items-start gap-3">
              <Check className="w-6 h-6 text-emerald-400 mt-0.5" />
              <div className="flex-1">
                <h3 className="font-semibold text-emerald-400 mb-2">Password Reset Successfully!</h3>
                <p className="text-sm text-muted-foreground mb-4">
                  Save this password - it won't be shown again.
                </p>
                <div className="bg-secondary/50 rounded-lg p-4 space-y-3">
                  <div>
                    <p className="text-xs text-muted-foreground mb-1">Database</p>
                    <code className="text-sm">{showNewPassword.db.name}</code>
                  </div>
                  <div>
                    <p className="text-xs text-muted-foreground mb-1">Username</p>
                    <code className="text-sm">{showNewPassword.db.username}</code>
                  </div>
                  <div>
                    <p className="text-xs text-muted-foreground mb-1">New Password</p>
                    <div className="flex items-center gap-2">
                      <code className="text-sm font-mono break-all">{showNewPassword.password}</code>
                      <button
                        onClick={() => copyToClipboard(showNewPassword.password, 'newpass')}
                        className="text-muted-foreground hover:text-foreground flex-shrink-0"
                      >
                        {copiedId === 'newpass' ? <Check className="w-4 h-4 text-emerald-400" /> : <Copy className="w-4 h-4" />}
                      </button>
                    </div>
                  </div>
                </div>
                <button
                  onClick={() => setShowNewPassword(null)}
                  className="mt-4 w-full px-4 py-2 bg-secondary hover:bg-secondary/80 rounded-lg transition-colors"
                >
                  Done
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Create Modal */}
      {showCreateModal && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
          <div className="bg-card border border-border rounded-xl w-full max-w-md p-6 m-4">
            <h2 className="text-xl font-semibold mb-4">Create Database</h2>

            {error && (
              <div className="bg-red-500/10 border border-red-500/20 text-red-400 px-4 py-3 rounded-lg mb-4 flex items-center gap-2">
                <AlertTriangle className="w-4 h-4" />
                {error}
              </div>
            )}

            <form onSubmit={handleCreate} className="space-y-4">
              <div>
                <label className="block text-sm font-medium mb-2">Database Name</label>
                <input
                  type="text"
                  value={form.name}
                  onChange={(e) => setForm({ ...form, name: e.target.value })}
                  className="w-full px-3 py-2 bg-secondary border border-border rounded-lg focus:outline-none focus:ring-2 focus:ring-emerald-500"
                  placeholder="my_database"
                  required
                  pattern="[a-zA-Z0-9_]+"
                  title="Only letters, numbers, and underscores"
                />
              </div>

              <div>
                <label className="block text-sm font-medium mb-2">Username (optional)</label>
                <input
                  type="text"
                  value={form.username}
                  onChange={(e) => setForm({ ...form, username: e.target.value })}
                  className="w-full px-3 py-2 bg-secondary border border-border rounded-lg focus:outline-none focus:ring-2 focus:ring-emerald-500"
                  placeholder="Same as database name"
                  pattern="[a-zA-Z0-9_]+"
                />
              </div>

              <div>
                <label className="block text-sm font-medium mb-2">Password (optional)</label>
                <input
                  type="text"
                  value={form.password}
                  onChange={(e) => setForm({ ...form, password: e.target.value })}
                  className="w-full px-3 py-2 bg-secondary border border-border rounded-lg focus:outline-none focus:ring-2 focus:ring-emerald-500"
                  placeholder="Auto-generated if empty"
                />
                <p className="text-xs text-muted-foreground mt-1">
                  Leave empty to generate a strong password
                </p>
              </div>

              <div className="flex gap-3 pt-4">
                <button
                  type="button"
                  onClick={() => {
                    setShowCreateModal(false)
                    setError('')
                    setForm({ name: '', username: '', password: '' })
                  }}
                  className="flex-1 px-4 py-2 bg-secondary hover:bg-secondary/80 rounded-lg transition-colors"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={creating}
                  className="flex-1 flex items-center justify-center gap-2 px-4 py-2 bg-emerald-500 hover:bg-emerald-600 text-white rounded-lg transition-colors disabled:opacity-50"
                >
                  {creating ? (
                    <>
                      <Loader2 className="w-4 h-4 animate-spin" />
                      Creating...
                    </>
                  ) : (
                    <>
                      <Plus className="w-4 h-4" />
                      Create
                    </>
                  )}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* Reset Password Modal */}
      {showResetPasswordModal && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
          <div className="bg-card border border-border rounded-xl w-full max-w-md p-6 m-4">
            <h2 className="text-xl font-semibold mb-4">Reset Database Password</h2>
            <p className="text-sm text-muted-foreground mb-4">
              Reset password for database user <code className="text-foreground">{showResetPasswordModal.username}</code>
            </p>

            {error && (
              <div className="bg-red-500/10 border border-red-500/20 text-red-400 px-4 py-3 rounded-lg mb-4 flex items-center gap-2">
                <AlertTriangle className="w-4 h-4" />
                {error}
              </div>
            )}

            <form onSubmit={handleResetPassword} className="space-y-4">
              <div>
                <label className="block text-sm font-medium mb-2">New Password</label>
                <div className="flex gap-2">
                  <input
                    type="text"
                    value={newPassword}
                    onChange={(e) => setNewPassword(e.target.value)}
                    className="flex-1 px-3 py-2 bg-secondary border border-border rounded-lg focus:outline-none focus:ring-2 focus:ring-emerald-500 font-mono text-sm"
                    placeholder="Enter new password"
                    required
                    minLength={8}
                  />
                  <button
                    type="button"
                    onClick={generateRandomPassword}
                    className="px-3 py-2 bg-secondary hover:bg-secondary/80 border border-border rounded-lg transition-colors text-sm"
                    title="Generate random password"
                  >
                    Generate
                  </button>
                </div>
              </div>

              <div className="flex gap-3 pt-4">
                <button
                  type="button"
                  onClick={() => {
                    setShowResetPasswordModal(null)
                    setError('')
                    setNewPassword('')
                  }}
                  className="flex-1 px-4 py-2 bg-secondary hover:bg-secondary/80 rounded-lg transition-colors"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={resettingPassword}
                  className="flex-1 flex items-center justify-center gap-2 px-4 py-2 bg-amber-500 hover:bg-amber-600 text-white rounded-lg transition-colors disabled:opacity-50"
                >
                  {resettingPassword ? (
                    <>
                      <Loader2 className="w-4 h-4 animate-spin" />
                      Resetting...
                    </>
                  ) : (
                    <>
                      <Key className="w-4 h-4" />
                      Reset Password
                    </>
                  )}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  )
}

