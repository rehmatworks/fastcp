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
  Search,
  X,
  AlertCircle,
} from 'lucide-react'
import { api, MySQLInstallStatus } from '@/lib/api'
import { cn } from '@/lib/utils'
import { useAuth } from '@/contexts/AuthContext'
import { formatDate } from '@/lib/utils'

interface DatabaseItem {
  id: string
  user_id: string
  site_id?: string
  name: string
  username: string
  password?: string
  host: string
  port: number
  type: string
  created_at: string
}

interface DatabaseStatus {
  mysql: {
    installed: boolean
    running: boolean
    version?: string
    database_count: number
  }
  postgresql: {
    installed: boolean
    running: boolean
    version?: string
    database_count: number
  }
}

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

export function DatabasesPage() {
  const { user } = useAuth()
  const [databases, setDatabases] = useState<DatabaseItem[]>([])
  const [status, setStatus] = useState<DatabaseStatus | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [search, setSearch] = useState('')
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
  const [deleteConfirm, setDeleteConfirm] = useState<DatabaseItem | null>(null)
  const [installConfirm, setInstallConfirm] = useState<string | null>(null)
  const [deleting, setDeleting] = useState(false)
  const pollIntervalRef = useRef<NodeJS.Timeout | null>(null)

  const [form, setForm] = useState({
    name: '',
    username: '',
    password: '',
    type: 'mysql',
  })

  const pollInstallStatus = useCallback(async (dbType: string) => {
    try {
      let response
      if (dbType === 'mysql') {
        response = await api.getMySQLInstallStatus()
      } else if (dbType === 'postgresql') {
        response = await api.getPostgreSQLInstallStatus()
      }

      if (!response) {
        setInstalling(false)
        setInstallStatus({ in_progress: false, success: false, error: 'Failed to get installation status' })
        if (pollIntervalRef.current) {
          clearInterval(pollIntervalRef.current)
        }
        return
      }

      if (!response.in_progress && response.success) {
        setInstalling(false)
        setInstallStatus({ in_progress: false, success: true, message: response.message })
        fetchData()
        if (pollIntervalRef.current) {
          clearInterval(pollIntervalRef.current)
        }
      } else if (!response.in_progress && !response.success) {
        setInstalling(false)
        setInstallStatus({ in_progress: false, success: false, error: response.error || response.message })
        if (pollIntervalRef.current) {
          clearInterval(pollIntervalRef.current)
        }
      } else {
        setInstallStatus({ in_progress: true, success: false, message: response.message })
      }
    } catch (error: any) {
      setInstalling(false)
      setInstallStatus({ in_progress: false, success: false, error: error.message || 'Unknown error' })
      if (pollIntervalRef.current) {
        clearInterval(pollIntervalRef.current)
      }
    }
  }, [])

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
        type: form.type,
      })
      setNewDatabase(created)
      setShowCreateModal(false)
      setForm({ name: '', username: '', password: '', type: 'mysql' })
      fetchData()
    } catch (err: any) {
      setError(err.message || 'Failed to create database')
    } finally {
      setCreating(false)
    }
  }

  async function handleDelete() {
    if (!deleteConfirm) return
    setDeleting(true)

    try {
      await api.deleteDatabase(deleteConfirm.id)
      setDeleteConfirm(null)
      fetchData()
    } catch (error) {
      console.error('Failed to delete database:', error)
    } finally {
      setDeleting(false)
    }
  }

  async function handleInstall(dbType: string) {
    setInstallConfirm(null)
    setInstalling(true)
    setInstallStatus({ in_progress: true, success: false, message: `Starting ${dbType} installation...` })

    try {
      let response
      if (dbType === 'mysql') {
        response = await api.installMySQL()
      } else if (dbType === 'postgresql') {
        response = await api.installPostgreSQL()
      }

      if (response && response.status === 'completed') {
        setInstalling(false)
        setInstallStatus({ in_progress: false, success: true, message: response.message })
        fetchData()
        return
      }

      pollIntervalRef.current = setInterval(() => pollInstallStatus(dbType), 2000)
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
      if (navigator.clipboard && navigator.clipboard.writeText) {
        await navigator.clipboard.writeText(text)
      } else {
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
    }
  }

  const filteredDatabases = databases.filter(
    (db) =>
      db.name.toLowerCase().includes(search.toLowerCase()) ||
      db.username.toLowerCase().includes(search.toLowerCase()) ||
      db.type.toLowerCase().includes(search.toLowerCase())
  )

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="w-10 h-10 border-2 border-primary border-t-transparent rounded-full animate-spin" />
      </div>
    )
  }

  // Database servers not installed
  if (status && !status.mysql.installed && !status.postgresql.installed) {
    return (
      <div className="space-y-6">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Databases</h1>
          <p className="text-muted-foreground mt-1">Manage MySQL and PostgreSQL databases</p>
        </div>

        <div className="bg-card border border-border rounded-2xl p-12 text-center card-shadow">
          <div className="w-16 h-16 rounded-2xl bg-blue-500/10 flex items-center justify-center mx-auto mb-5">
            <Server className="w-8 h-8 text-blue-600 dark:text-blue-400" />
          </div>
          <h2 className="text-xl font-semibold mb-2">Database Servers Not Installed</h2>
          <p className="text-muted-foreground mb-6 max-w-md mx-auto">
            No database servers are installed. Install MySQL or PostgreSQL to start creating databases.
          </p>

          {installing && installStatus?.in_progress && (
            <div className="mb-6 p-4 bg-blue-500/10 border border-blue-500/20 rounded-xl max-w-md mx-auto">
              <div className="flex items-center gap-3">
                <Loader2 className="w-5 h-5 animate-spin text-blue-500" />
                <div className="text-left">
                  <p className="text-blue-700 dark:text-blue-400 font-medium">Installing database server...</p>
                  <p className="text-sm text-muted-foreground">
                    {installStatus.message || 'This may take a few minutes...'}
                  </p>
                </div>
              </div>
            </div>
          )}

          {installStatus && !installStatus.in_progress && installStatus.error && (
            <div className="mb-6 p-4 bg-red-500/10 border border-red-500/20 rounded-xl max-w-md mx-auto">
              <div className="flex items-start gap-3">
                <AlertTriangle className="w-5 h-5 text-red-500 mt-0.5" />
                <div className="text-left">
                  <p className="text-red-700 dark:text-red-400 font-medium">Installation Failed</p>
                  <p className="text-sm text-muted-foreground">{installStatus.error}</p>
                </div>
              </div>
            </div>
          )}

          {installStatus && !installStatus.in_progress && installStatus.success && (
            <div className="mb-6 p-4 bg-emerald-500/10 border border-emerald-500/20 rounded-xl max-w-md mx-auto">
              <div className="flex items-center gap-3">
                <Check className="w-5 h-5 text-emerald-500" />
                <div className="text-left">
                  <p className="text-emerald-700 dark:text-emerald-400 font-medium">Database Server Installed!</p>
                  <p className="text-sm text-muted-foreground">{installStatus.message}</p>
                </div>
              </div>
            </div>
          )}

          {!installing && user?.role === 'admin' && (
            <div className="flex gap-3 justify-center">
              <button
                onClick={() => setInstallConfirm('mysql')}
                className="inline-flex items-center gap-2 px-5 py-2.5 bg-primary hover:bg-primary/90 text-primary-foreground font-medium rounded-xl transition-colors"
              >
                <Server className="w-5 h-5" />
                Install MySQL
              </button>
              <button
                onClick={() => setInstallConfirm('postgresql')}
                className="inline-flex items-center gap-2 px-5 py-2.5 bg-secondary hover:bg-secondary/80 text-secondary-foreground font-medium rounded-xl transition-colors"
              >
                <Server className="w-5 h-5" />
                Install PostgreSQL
              </button>
            </div>
          )}
          {!installing && user?.role !== 'admin' && (
            <p className="text-amber-600 dark:text-amber-400">Contact an administrator to install database servers.</p>
          )}
        </div>

        <ConfirmModal
          isOpen={installConfirm !== null}
          title={`Install ${installConfirm === 'mysql' ? 'MySQL' : 'PostgreSQL'} Server`}
          message={`This will download and configure ${installConfirm === 'mysql' ? 'MySQL' : 'PostgreSQL'} on your server. The process may take a few minutes.`}
          confirmLabel={`Install ${installConfirm === 'mysql' ? 'MySQL' : 'PostgreSQL'}`}
          confirmVariant="primary"
          onConfirm={installConfirm === 'mysql' ? () => handleInstall('mysql') : () => handleInstall('postgresql')}
          onCancel={() => setInstallConfirm(null)}
        />
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Databases</h1>
          <p className="text-muted-foreground mt-1">
            Manage your MySQL and PostgreSQL databases
          </p>
        </div>
        <button
          onClick={() => setShowCreateModal(true)}
          className="flex items-center justify-center gap-2 px-4 py-2.5 bg-primary hover:bg-primary/90 text-primary-foreground font-medium rounded-xl transition-colors"
        >
          <Plus className="w-4 h-4" />
          New Database
        </button>
      </div>

      {/* Search */}
      <div className="relative">
        <Search className="absolute left-4 top-1/2 -translate-y-1/2 w-5 h-5 text-muted-foreground" />
        <input
          type="text"
          placeholder="Search databases..."
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="w-full pl-12 pr-4 py-3 bg-card border border-border rounded-xl focus:outline-none focus:ring-2 focus:ring-primary/50 focus:border-primary transition-colors"
        />
      </div>

      {/* New Database Credentials */}
      {newDatabase && newDatabase.password && (
        <div className="bg-emerald-500/10 border border-emerald-500/20 rounded-2xl p-5">
          <div className="flex items-start gap-4">
            <div className="w-10 h-10 rounded-xl bg-emerald-500/20 flex items-center justify-center flex-shrink-0">
              <Check className="w-5 h-5 text-emerald-500" />
            </div>
            <div className="flex-1">
              <h3 className="font-semibold text-emerald-700 dark:text-emerald-400 mb-1">Database Created!</h3>
              <p className="text-sm text-muted-foreground mb-4">
                Save these credentials - the password won't be shown again.
              </p>
              <div className="grid grid-cols-2 gap-4 bg-card rounded-xl p-4 border border-border">
                <div>
                  <p className="text-xs text-muted-foreground mb-1">Database Name</p>
                  <div className="flex items-center gap-2">
                    <code className="text-sm font-mono text-foreground">{newDatabase.name}</code>
                    <button
                      onClick={() => copyToClipboard(newDatabase.name, 'name')}
                      className="text-muted-foreground hover:text-foreground"
                    >
                      {copiedId === 'name' ? <Check className="w-4 h-4 text-emerald-500" /> : <Copy className="w-4 h-4" />}
                    </button>
                  </div>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground mb-1">Username</p>
                  <div className="flex items-center gap-2">
                    <code className="text-sm font-mono text-foreground">{newDatabase.username}</code>
                    <button
                      onClick={() => copyToClipboard(newDatabase.username, 'user')}
                      className="text-muted-foreground hover:text-foreground"
                    >
                      {copiedId === 'user' ? <Check className="w-4 h-4 text-emerald-500" /> : <Copy className="w-4 h-4" />}
                    </button>
                  </div>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground mb-1">Password</p>
                  <div className="flex items-center gap-2">
                    <code className="text-sm font-mono text-foreground">{newDatabase.password}</code>
                    <button
                      onClick={() => copyToClipboard(newDatabase.password!, 'pass')}
                      className="text-muted-foreground hover:text-foreground"
                    >
                      {copiedId === 'pass' ? <Check className="w-4 h-4 text-emerald-500" /> : <Copy className="w-4 h-4" />}
                    </button>
                  </div>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground mb-1">Host</p>
                  <code className="text-sm font-mono text-foreground">{newDatabase.host}:{newDatabase.port}</code>
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
      {filteredDatabases.length === 0 ? (
        <div className="bg-card border border-border rounded-2xl p-12 text-center card-shadow">
          <div className="w-16 h-16 rounded-2xl bg-secondary flex items-center justify-center mx-auto mb-4">
            {databases.length === 0 ? (
              <Database className="w-8 h-8 text-muted-foreground" />
            ) : (
              <Search className="w-8 h-8 text-muted-foreground" />
            )}
          </div>
          <h3 className="font-semibold text-lg mb-2">
            {databases.length === 0 ? 'No Databases Yet' : 'No Results'}
          </h3>
          <p className="text-muted-foreground mb-6 max-w-sm mx-auto">
            {databases.length === 0
              ? 'Create your first database to get started'
              : 'Try a different search term'}
          </p>
          {databases.length === 0 && (
            <button
              onClick={() => setShowCreateModal(true)}
              className="inline-flex items-center gap-2 px-4 py-2.5 bg-primary hover:bg-primary/90 text-primary-foreground font-medium rounded-xl transition-colors"
            >
              <Plus className="w-4 h-4" />
              Create Database
            </button>
          )}
        </div>
      ) : (
        <div className="bg-card border border-border rounded-2xl overflow-hidden card-shadow">
          <div className="overflow-x-auto">
            <table className="w-full">
              <thead>
                <tr className="border-b border-border bg-secondary/50">
                  <th className="text-left px-5 py-3 text-xs font-medium text-muted-foreground uppercase tracking-wider">
                    Database
                  </th>
                  <th className="text-left px-5 py-3 text-xs font-medium text-muted-foreground uppercase tracking-wider">
                    Type
                  </th>
                  <th className="text-left px-5 py-3 text-xs font-medium text-muted-foreground uppercase tracking-wider">
                    Username
                  </th>
                  <th className="text-left px-5 py-3 text-xs font-medium text-muted-foreground uppercase tracking-wider">
                    Host
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
                {filteredDatabases.map((db) => (
                  <tr key={db.id} className="hover:bg-secondary/30 transition-colors">
                    <td className="px-5 py-4">
                      <div className="flex items-center gap-3">
                        <div className="w-10 h-10 rounded-xl bg-blue-500/10 flex items-center justify-center">
                          <Database className="w-5 h-5 text-blue-600 dark:text-blue-400" />
                        </div>
                        <span className="font-medium font-mono">{db.name}</span>
                      </div>
                    </td>
                    <td className="px-5 py-4">
                      <span className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${db.type === 'mysql'
                        ? 'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-300'
                        : 'bg-purple-100 text-purple-800 dark:bg-purple-900 dark:text-purple-300'
                        }`}>
                        {db.type === 'mysql' ? 'MySQL' : 'PostgreSQL'}
                      </span>
                    </td>
                    <td className="px-5 py-4">
                      <code className="text-sm bg-secondary px-2 py-1 rounded">
                        {db.username}
                      </code>
                    </td>
                    <td className="px-5 py-4 text-sm text-muted-foreground font-mono">
                      {db.host}:{db.port}
                    </td>
                    <td className="px-5 py-4 text-sm text-muted-foreground">
                      {formatDate(db.created_at)}
                    </td>
                    <td className="px-5 py-4">
                      <div className="flex items-center justify-end gap-1">
                        <button
                          onClick={() => {
                            setShowResetPasswordModal(db)
                            setNewPassword('')
                            setError('')
                          }}
                          className="p-2 text-muted-foreground hover:text-amber-500 hover:bg-amber-500/10 rounded-lg transition-colors"
                          title="Reset password"
                        >
                          <Key className="w-4 h-4" />
                        </button>
                        <button
                          onClick={() => setDeleteConfirm(db)}
                          className="p-2 text-muted-foreground hover:text-red-500 hover:bg-red-500/10 rounded-lg transition-colors"
                          title="Delete database"
                        >
                          <Trash2 className="w-4 h-4" />
                        </button>
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
        title="Delete Database"
        message={`Are you sure you want to delete "${deleteConfirm?.name}"? This action cannot be undone and all data will be permanently lost.`}
        confirmLabel="Delete Database"
        confirmVariant="danger"
        isLoading={deleting}
        onConfirm={handleDelete}
        onCancel={() => setDeleteConfirm(null)}
      />

      {/* New Password Display */}
      {showNewPassword && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
          <div className="bg-card border border-border rounded-2xl w-full max-w-md shadow-xl">
            <div className="p-6">
              <div className="flex items-start gap-4">
                <div className="w-10 h-10 rounded-xl bg-emerald-500/10 flex items-center justify-center flex-shrink-0">
                  <Check className="w-5 h-5 text-emerald-500" />
                </div>
                <div className="flex-1">
                  <h3 className="font-semibold text-emerald-700 dark:text-emerald-400 mb-1">Password Reset!</h3>
                  <p className="text-sm text-muted-foreground mb-4">
                    Save this password - it won't be shown again.
                  </p>
                  <div className="bg-secondary rounded-xl p-4 space-y-3">
                    <div>
                      <p className="text-xs text-muted-foreground mb-1">Database</p>
                      <code className="text-sm font-mono">{showNewPassword.db.name}</code>
                    </div>
                    <div>
                      <p className="text-xs text-muted-foreground mb-1">Username</p>
                      <code className="text-sm font-mono">{showNewPassword.db.username}</code>
                    </div>
                    <div>
                      <p className="text-xs text-muted-foreground mb-1">New Password</p>
                      <div className="flex items-center gap-2">
                        <code className="text-sm font-mono break-all">{showNewPassword.password}</code>
                        <button
                          onClick={() => copyToClipboard(showNewPassword.password, 'newpass')}
                          className="text-muted-foreground hover:text-foreground flex-shrink-0"
                        >
                          {copiedId === 'newpass' ? <Check className="w-4 h-4 text-emerald-500" /> : <Copy className="w-4 h-4" />}
                        </button>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            </div>
            <div className="px-6 pb-6">
              <button
                onClick={() => setShowNewPassword(null)}
                className="w-full px-4 py-2.5 bg-secondary hover:bg-secondary/80 rounded-xl font-medium transition-colors"
              >
                Done
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Create Modal */}
      {showCreateModal && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
          <div className="bg-card border border-border rounded-2xl w-full max-w-md shadow-xl">
            <div className="flex items-center justify-between px-6 py-4 border-b border-border">
              <h2 className="text-lg font-semibold">Create Database</h2>
              <button
                onClick={() => {
                  setShowCreateModal(false)
                  setError('')
                  setForm({ name: '', username: '', password: '', type: 'mysql' })
                }}
                className="p-2 hover:bg-secondary rounded-lg transition-colors"
              >
                <X className="w-5 h-5" />
              </button>
            </div>

            <form onSubmit={handleCreate} className="p-6 space-y-4">
              {error && (
                <div className="bg-red-500/10 border border-red-500/20 text-red-600 dark:text-red-400 px-4 py-3 rounded-xl flex items-center gap-2">
                  <AlertTriangle className="w-4 h-4 flex-shrink-0" />
                  <span className="text-sm">{error}</span>
                </div>
              )}

              <div>
                <label className="block text-sm font-medium mb-2">Database Name</label>
                <input
                  type="text"
                  value={form.name}
                  onChange={(e) => setForm({ ...form, name: e.target.value })}
                  className="w-full px-4 py-3 bg-secondary/50 border border-border rounded-xl focus:outline-none focus:ring-2 focus:ring-primary/50 focus:border-primary font-mono transition-colors"
                  placeholder="my_database"
                  required
                  pattern="[a-zA-Z0-9_]+"
                  title="Only letters, numbers, and underscores"
                />
              </div>

              <div>
                <label className="block text-sm font-medium mb-2">Username <span className="text-muted-foreground font-normal">(optional)</span></label>
                <input
                  type="text"
                  value={form.username}
                  onChange={(e) => setForm({ ...form, username: e.target.value })}
                  className="w-full px-4 py-3 bg-secondary/50 border border-border rounded-xl focus:outline-none focus:ring-2 focus:ring-primary/50 focus:border-primary font-mono transition-colors"
                  placeholder="Same as database name"
                  pattern="[a-zA-Z0-9_]+"
                />
              </div>

              <div>
                <label className="block text-sm font-medium mb-2">Database Type</label>
                <select
                  value={form.type}
                  onChange={(e) => setForm({ ...form, type: e.target.value as 'mysql' | 'postgresql' })}
                  className="w-full px-4 py-3 bg-secondary/50 border border-border rounded-xl focus:outline-none focus:ring-2 focus:ring-primary/50 focus:border-primary transition-colors"
                >
                  <option value="mysql">MySQL</option>
                  <option value="postgresql">PostgreSQL</option>
                </select>
              </div>

              <div className="flex gap-3 pt-2">
                <button
                  type="button"
                  onClick={() => {
                    setShowCreateModal(false)
                    setError('')
                    setForm({ name: '', username: '', password: '', type: 'mysql' })
                  }}
                  className="flex-1 px-4 py-2.5 bg-secondary hover:bg-secondary/80 rounded-xl font-medium transition-colors"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={creating}
                  className="flex-1 flex items-center justify-center gap-2 px-4 py-2.5 bg-primary hover:bg-primary/90 text-primary-foreground font-medium rounded-xl transition-colors disabled:opacity-50"
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
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
          <div className="bg-card border border-border rounded-2xl w-full max-w-md shadow-xl">
            <div className="flex items-center justify-between px-6 py-4 border-b border-border">
              <h2 className="text-lg font-semibold">Reset Password</h2>
              <button
                onClick={() => {
                  setShowResetPasswordModal(null)
                  setError('')
                  setNewPassword('')
                }}
                className="p-2 hover:bg-secondary rounded-lg transition-colors"
              >
                <X className="w-5 h-5" />
              </button>
            </div>

            <form onSubmit={handleResetPassword} className="p-6 space-y-4">
              <p className="text-sm text-muted-foreground">
                Reset password for <code className="text-foreground font-mono bg-secondary px-1.5 py-0.5 rounded">{showResetPasswordModal.username}</code>
              </p>

              {error && (
                <div className="bg-red-500/10 border border-red-500/20 text-red-600 dark:text-red-400 px-4 py-3 rounded-xl flex items-center gap-2">
                  <AlertTriangle className="w-4 h-4 flex-shrink-0" />
                  <span className="text-sm">{error}</span>
                </div>
              )}

              <div>
                <label className="block text-sm font-medium mb-2">New Password</label>
                <div className="flex gap-2">
                  <input
                    type="text"
                    value={newPassword}
                    onChange={(e) => setNewPassword(e.target.value)}
                    className="flex-1 px-4 py-3 bg-secondary/50 border border-border rounded-xl focus:outline-none focus:ring-2 focus:ring-primary/50 focus:border-primary font-mono text-sm transition-colors"
                    placeholder="Enter new password"
                    required
                    minLength={8}
                  />
                  <button
                    type="button"
                    onClick={generateRandomPassword}
                    className="px-4 py-2 bg-secondary hover:bg-secondary/80 rounded-xl transition-colors text-sm font-medium"
                    title="Generate random password"
                  >
                    Generate
                  </button>
                </div>
              </div>

              <div className="flex gap-3 pt-2">
                <button
                  type="button"
                  onClick={() => {
                    setShowResetPasswordModal(null)
                    setError('')
                    setNewPassword('')
                  }}
                  className="flex-1 px-4 py-2.5 bg-secondary hover:bg-secondary/80 rounded-xl font-medium transition-colors"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={resettingPassword}
                  className="flex-1 flex items-center justify-center gap-2 px-4 py-2.5 bg-amber-500 hover:bg-amber-600 text-white font-medium rounded-xl transition-colors disabled:opacity-50"
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
