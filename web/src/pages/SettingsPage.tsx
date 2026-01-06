import { useEffect, useState } from 'react'
import {
  Key,
  Plus,
  Trash2,
  Copy,
  Check,
  Shield,
  RefreshCw,
  Loader2,
  X,
  AlertCircle,
  Server,
  Lock,
  Terminal,
  Upload,
  Eye,
  EyeOff,
} from 'lucide-react'
import { api, ConnectionInfo, SSHKeyItem } from '@/lib/api'
import { formatDate } from '@/lib/utils'
import { useAuth } from '@/contexts/AuthContext'
import type { APIKey } from '@/types'

interface ConfirmModalProps {
  isOpen: boolean
  title: string
  message: string
  confirmLabel: string
  isLoading?: boolean
  onConfirm: () => void
  onCancel: () => void
}

function ConfirmModal({
  isOpen,
  title,
  message,
  confirmLabel,
  isLoading,
  onConfirm,
  onCancel,
}: ConfirmModalProps) {
  if (!isOpen) return null

  return (
    <div className="fixed inset-0 bg-black/60 backdrop-blur-sm flex items-center justify-center z-50 p-4">
      <div className="bg-card border border-border rounded-2xl w-full max-w-md shadow-xl">
        <div className="p-6">
          <div className="flex items-start gap-4">
            <div className="w-12 h-12 rounded-xl bg-red-500/10 flex items-center justify-center flex-shrink-0">
              <AlertCircle className="w-6 h-6 text-red-500" />
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
            className="flex-1 flex items-center justify-center gap-2 px-4 py-2.5 bg-red-500 hover:bg-red-600 text-white rounded-xl font-medium transition-colors disabled:opacity-50"
          >
            {isLoading ? (
              <>
                <Loader2 className="w-4 h-4 animate-spin" />
                Deleting...
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

export function SettingsPage() {
  const { user } = useAuth()
  const [apiKeys, setAPIKeys] = useState<APIKey[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [showCreateModal, setShowCreateModal] = useState(false)
  const [newKey, setNewKey] = useState<APIKey | null>(null)
  const [newKeyName, setNewKeyName] = useState('')
  const [isCreating, setIsCreating] = useState(false)
  const [copiedKey, setCopiedKey] = useState<string | null>(null)
  const [isReloading, setIsReloading] = useState(false)
  const [deleteConfirm, setDeleteConfirm] = useState<APIKey | null>(null)
  const [isDeleting, setIsDeleting] = useState(false)

  // Connection info state
  const [connectionInfo, setConnectionInfo] = useState<ConnectionInfo | null>(null)
  
  // Password change state
  const [showPasswordModal, setShowPasswordModal] = useState(false)
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [showCurrentPassword, setShowCurrentPassword] = useState(false)
  const [showNewPassword, setShowNewPassword] = useState(false)
  const [isChangingPassword, setIsChangingPassword] = useState(false)
  const [passwordError, setPasswordError] = useState('')
  const [passwordSuccess, setPasswordSuccess] = useState('')

  // SSH Keys state
  const [sshKeys, setSSHKeys] = useState<SSHKeyItem[]>([])
  const [showSSHKeyModal, setShowSSHKeyModal] = useState(false)
  const [sshKeyName, setSSHKeyName] = useState('')
  const [sshKeyPublic, setSSHKeyPublic] = useState('')
  const [isAddingSSHKey, setIsAddingSSHKey] = useState(false)
  const [sshKeyError, setSSHKeyError] = useState('')
  const [deleteSSHKeyConfirm, setDeleteSSHKeyConfirm] = useState<SSHKeyItem | null>(null)
  const [isDeletingSSHKey, setIsDeletingSSHKey] = useState(false)

  useEffect(() => {
    fetchData()
  }, [])

  async function fetchData() {
    setIsLoading(true)
    try {
      await Promise.all([
        fetchAPIKeys(),
        fetchConnectionInfo(),
        fetchSSHKeys(),
      ])
    } finally {
      setIsLoading(false)
    }
  }

  async function fetchConnectionInfo() {
    try {
      const info = await api.getConnectionInfo()
      setConnectionInfo(info)
    } catch (error) {
      console.error('Failed to fetch connection info:', error)
    }
  }

  async function fetchSSHKeys() {
    try {
      const data = await api.getSSHKeys()
      setSSHKeys(data.ssh_keys || [])
    } catch (error) {
      console.error('Failed to fetch SSH keys:', error)
    }
  }

  async function fetchAPIKeys() {
    try {
      const data = await api.getAPIKeys()
      setAPIKeys(data.api_keys || [])
    } catch (error) {
      console.error('Failed to fetch API keys:', error)
    }
  }

  async function handleChangePassword() {
    setPasswordError('')
    setPasswordSuccess('')

    if (newPassword !== confirmPassword) {
      setPasswordError('Passwords do not match')
      return
    }

    if (newPassword.length < 8) {
      setPasswordError('Password must be at least 8 characters')
      return
    }

    setIsChangingPassword(true)
    try {
      await api.changePassword(currentPassword, newPassword)
      setPasswordSuccess('Password changed successfully')
      setCurrentPassword('')
      setNewPassword('')
      setConfirmPassword('')
      setTimeout(() => {
        setShowPasswordModal(false)
        setPasswordSuccess('')
      }, 2000)
    } catch (error) {
      setPasswordError(error instanceof Error ? error.message : 'Failed to change password')
    } finally {
      setIsChangingPassword(false)
    }
  }

  async function handleAddSSHKey() {
    setSSHKeyError('')
    
    if (!sshKeyName.trim() || !sshKeyPublic.trim()) {
      setSSHKeyError('Name and public key are required')
      return
    }

    setIsAddingSSHKey(true)
    try {
      await api.addSSHKey(sshKeyName, sshKeyPublic)
      setSSHKeyName('')
      setSSHKeyPublic('')
      setShowSSHKeyModal(false)
      fetchSSHKeys()
    } catch (error) {
      setSSHKeyError(error instanceof Error ? error.message : 'Failed to add SSH key')
    } finally {
      setIsAddingSSHKey(false)
    }
  }

  async function handleDeleteSSHKey() {
    if (!deleteSSHKeyConfirm) return
    setIsDeletingSSHKey(true)
    try {
      await api.deleteSSHKey(deleteSSHKeyConfirm.fingerprint)
      setDeleteSSHKeyConfirm(null)
      fetchSSHKeys()
    } catch (error) {
      console.error('Failed to delete SSH key:', error)
    } finally {
      setIsDeletingSSHKey(false)
    }
  }

  async function handleCreateKey() {
    if (!newKeyName.trim()) return
    setIsCreating(true)

    try {
      const key = await api.createAPIKey(newKeyName, ['sites:read', 'sites:write'])
      setNewKey(key)
      setNewKeyName('')
      fetchAPIKeys()
    } catch (error) {
      console.error('Failed to create API key:', error)
    } finally {
      setIsCreating(false)
    }
  }

  async function handleDeleteKey() {
    if (!deleteConfirm) return
    setIsDeleting(true)

    try {
      await api.deleteAPIKey(deleteConfirm.id)
      setDeleteConfirm(null)
      fetchAPIKeys()
    } catch (error) {
      console.error('Failed to delete API key:', error)
    } finally {
      setIsDeleting(false)
    }
  }

  async function handleReloadAll() {
    setIsReloading(true)
    try {
      await api.reloadAll()
    } catch (error) {
      console.error('Failed to reload:', error)
    } finally {
      setIsReloading(false)
    }
  }

  function copyToClipboard(text: string, id: string) {
    navigator.clipboard.writeText(text)
    setCopiedKey(id)
    setTimeout(() => setCopiedKey(null), 2000)
  }

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
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Settings</h1>
        <p className="text-muted-foreground mt-1">
          Manage API keys and system settings
        </p>
      </div>

      {/* Connection Info - SFTP/SSH Details */}
      <div className="bg-card border border-border rounded-2xl p-6 card-shadow">
        <div className="flex items-start gap-4 mb-6">
          <div className="w-12 h-12 rounded-xl bg-emerald-500/10 flex items-center justify-center flex-shrink-0">
            <Server className="w-6 h-6 text-emerald-600 dark:text-emerald-400" />
          </div>
          <div className="flex-1">
            <h3 className="font-semibold mb-1">SFTP / SSH Connection</h3>
            <p className="text-sm text-muted-foreground">
              Use these details to connect via SFTP or SSH
            </p>
          </div>
          <button
            onClick={() => setShowPasswordModal(true)}
            className="flex items-center gap-2 px-4 py-2 text-sm bg-secondary hover:bg-secondary/80 rounded-xl font-medium transition-colors"
          >
            <Lock className="w-4 h-4" />
            Change Password
          </button>
        </div>

        {connectionInfo && (
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div className="space-y-1">
              <label className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Host</label>
              <div className="flex items-center gap-2">
                <code className="flex-1 px-3 py-2 bg-secondary rounded-lg font-mono text-sm">{connectionInfo.host}</code>
                <button
                  onClick={() => copyToClipboard(connectionInfo.host, 'host')}
                  className="p-2 hover:bg-secondary rounded-lg transition-colors"
                >
                  {copiedKey === 'host' ? <Check className="w-4 h-4 text-emerald-500" /> : <Copy className="w-4 h-4" />}
                </button>
              </div>
            </div>
            <div className="space-y-1">
              <label className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Port</label>
              <div className="flex items-center gap-2">
                <code className="flex-1 px-3 py-2 bg-secondary rounded-lg font-mono text-sm">{connectionInfo.port}</code>
                <button
                  onClick={() => copyToClipboard(String(connectionInfo.port), 'port')}
                  className="p-2 hover:bg-secondary rounded-lg transition-colors"
                >
                  {copiedKey === 'port' ? <Check className="w-4 h-4 text-emerald-500" /> : <Copy className="w-4 h-4" />}
                </button>
              </div>
            </div>
            <div className="space-y-1">
              <label className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Username</label>
              <div className="flex items-center gap-2">
                <code className="flex-1 px-3 py-2 bg-secondary rounded-lg font-mono text-sm">{connectionInfo.username}</code>
                <button
                  onClick={() => copyToClipboard(connectionInfo.username, 'username')}
                  className="p-2 hover:bg-secondary rounded-lg transition-colors"
                >
                  {copiedKey === 'username' ? <Check className="w-4 h-4 text-emerald-500" /> : <Copy className="w-4 h-4" />}
                </button>
              </div>
            </div>
            <div className="space-y-1">
              <label className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Home Directory</label>
              <div className="flex items-center gap-2">
                <code className="flex-1 px-3 py-2 bg-secondary rounded-lg font-mono text-sm break-all">{connectionInfo.home_dir}</code>
                <button
                  onClick={() => copyToClipboard(connectionInfo.home_dir, 'home')}
                  className="p-2 hover:bg-secondary rounded-lg transition-colors"
                >
                  {copiedKey === 'home' ? <Check className="w-4 h-4 text-emerald-500" /> : <Copy className="w-4 h-4" />}
                </button>
              </div>
            </div>
          </div>
        )}

        {connectionInfo && (
          <div className="mt-4 flex items-center gap-4 text-sm">
            <span className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full ${connectionInfo.sftp_enabled ? 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400' : 'bg-red-500/10 text-red-500'}`}>
              <span className="w-1.5 h-1.5 rounded-full bg-current" />
              SFTP {connectionInfo.sftp_enabled ? 'Enabled' : 'Disabled'}
            </span>
            <span className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full ${connectionInfo.ssh_enabled ? 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400' : 'bg-amber-500/10 text-amber-600 dark:text-amber-400'}`}>
              <span className="w-1.5 h-1.5 rounded-full bg-current" />
              SSH {connectionInfo.ssh_enabled ? 'Enabled' : 'SFTP Only'}
            </span>
          </div>
        )}

        {connectionInfo?.note && (
          <p className="mt-3 text-xs text-muted-foreground">{connectionInfo.note}</p>
        )}
      </div>

      {/* SSH Keys */}
      <div className="bg-card border border-border rounded-2xl card-shadow overflow-hidden">
        <div className="flex items-center justify-between px-6 py-4 border-b border-border">
          <div className="flex items-center gap-3">
            <div className="w-10 h-10 rounded-xl bg-purple-500/10 flex items-center justify-center">
              <Terminal className="w-5 h-5 text-purple-600 dark:text-purple-400" />
            </div>
            <div>
              <h2 className="font-semibold">SSH Keys</h2>
              <p className="text-sm text-muted-foreground">
                Public keys for passwordless authentication
              </p>
            </div>
          </div>
          <button
            onClick={() => setShowSSHKeyModal(true)}
            className="flex items-center gap-2 px-4 py-2 text-sm bg-primary text-primary-foreground hover:bg-primary/90 rounded-xl font-medium transition-colors"
          >
            <Plus className="w-4 h-4" />
            Add Key
          </button>
        </div>

        <div className="divide-y divide-border">
          {sshKeys.length === 0 ? (
            <div className="px-6 py-12 text-center">
              <div className="w-16 h-16 rounded-2xl bg-secondary flex items-center justify-center mx-auto mb-4">
                <Terminal className="w-8 h-8 text-muted-foreground" />
              </div>
              <p className="text-muted-foreground mb-2">No SSH keys added</p>
              <button
                onClick={() => setShowSSHKeyModal(true)}
                className="text-sm text-primary hover:underline"
              >
                Add your first SSH key
              </button>
            </div>
          ) : (
            sshKeys.map((key) => (
              <div
                key={key.fingerprint}
                className="flex items-center justify-between px-6 py-4 hover:bg-secondary/30 transition-colors"
              >
                <div className="flex items-center gap-4 min-w-0 flex-1">
                  <div className="w-10 h-10 rounded-xl bg-secondary flex items-center justify-center flex-shrink-0">
                    <Key className="w-5 h-5 text-muted-foreground" />
                  </div>
                  <div className="min-w-0">
                    <p className="font-medium">{key.name || 'Unnamed Key'}</p>
                    <p className="text-sm text-muted-foreground font-mono truncate">
                      {key.fingerprint}
                    </p>
                  </div>
                </div>
                <button
                  onClick={() => setDeleteSSHKeyConfirm(key)}
                  className="p-2 text-muted-foreground hover:text-red-500 hover:bg-red-500/10 rounded-lg transition-colors flex-shrink-0"
                >
                  <Trash2 className="w-4 h-4" />
                </button>
              </div>
            ))
          )}
        </div>
      </div>

      {/* Quick Actions - Admin Only */}
      {user?.role === 'admin' && (
        <div className="bg-card border border-border rounded-2xl p-6 card-shadow">
        <h2 className="font-semibold mb-4">Quick Actions</h2>
        <div className="flex flex-wrap gap-3">
          <button
            onClick={handleReloadAll}
            disabled={isReloading}
              className="flex items-center gap-2 px-4 py-2.5 bg-secondary hover:bg-secondary/80 rounded-xl font-medium transition-colors disabled:opacity-50"
          >
            {isReloading ? (
              <Loader2 className="w-4 h-4 animate-spin" />
            ) : (
              <RefreshCw className="w-4 h-4" />
            )}
            Reload All Configurations
          </button>
        </div>
      </div>
      )}

      {/* API Keys - Admin Only */}
      {user?.role === 'admin' && (
        <div className="bg-card border border-border rounded-2xl card-shadow overflow-hidden">
          <div className="flex items-center justify-between px-6 py-4 border-b border-border">
            <div className="flex items-center gap-3">
              <div className="w-10 h-10 rounded-xl bg-primary/10 flex items-center justify-center">
                <Key className="w-5 h-5 text-primary" />
              </div>
              <div>
                <h2 className="font-semibold">API Keys</h2>
                <p className="text-sm text-muted-foreground">
                  For WHMCS and external integrations
                </p>
              </div>
            </div>
            <button
              onClick={() => setShowCreateModal(true)}
              className="flex items-center gap-2 px-4 py-2 text-sm bg-primary text-primary-foreground hover:bg-primary/90 rounded-xl font-medium transition-colors"
            >
              <Plus className="w-4 h-4" />
              Create Key
            </button>
          </div>

          <div className="divide-y divide-border">
            {apiKeys.length === 0 ? (
              <div className="px-6 py-12 text-center">
                <div className="w-16 h-16 rounded-2xl bg-secondary flex items-center justify-center mx-auto mb-4">
                  <Key className="w-8 h-8 text-muted-foreground" />
                </div>
                <p className="text-muted-foreground mb-2">No API keys yet</p>
                <button
                  onClick={() => setShowCreateModal(true)}
                  className="text-sm text-primary hover:underline"
                >
                  Create your first API key
                </button>
              </div>
            ) : (
              apiKeys.map((key) => (
                <div
                  key={key.id}
                  className="flex items-center justify-between px-6 py-4 hover:bg-secondary/30 transition-colors"
                >
                  <div className="flex items-center gap-4">
                    <div className="w-10 h-10 rounded-xl bg-secondary flex items-center justify-center">
                      <Shield className="w-5 h-5 text-muted-foreground" />
                    </div>
                    <div>
                      <p className="font-medium">{key.name}</p>
                      <p className="text-sm text-muted-foreground font-mono">
                        {key.key}
                      </p>
                    </div>
                  </div>
                  <div className="flex items-center gap-3">
                    <span className="text-xs text-muted-foreground">
                      {formatDate(key.created_at)}
                    </span>
                    <button
                      onClick={() => setDeleteConfirm(key)}
                      className="p-2 text-muted-foreground hover:text-red-500 hover:bg-red-500/10 rounded-lg transition-colors"
                    >
                      <Trash2 className="w-4 h-4" />
                    </button>
                  </div>
                </div>
              ))
            )}
          </div>
        </div>
      )}

      {/* WHMCS Integration Info - Admin Only */}
      {user?.role === 'admin' && (
        <div className="bg-card border border-border rounded-2xl p-6 card-shadow">
          <div className="flex items-start gap-4">
            <div className="w-12 h-12 rounded-xl bg-blue-500/10 flex items-center justify-center flex-shrink-0">
              <Shield className="w-6 h-6 text-blue-600 dark:text-blue-400" />
            </div>
            <div className="flex-1">
              <h3 className="font-semibold mb-1">WHMCS Integration</h3>
              <p className="text-sm text-muted-foreground mb-4">
                Use API keys to integrate FastCP with WHMCS for automated
                provisioning. Send requests to the following endpoints:
              </p>
              <div className="space-y-2">
                <div className="flex items-center gap-3 px-4 py-3 bg-secondary rounded-xl font-mono text-sm">
                  <span className="px-2 py-0.5 bg-emerald-500/20 text-emerald-700 dark:text-emerald-400 rounded text-xs font-semibold">POST</span>
                  <span className="text-foreground">/api/v1/whmcs/provision</span>
                </div>
                <div className="flex items-center gap-3 px-4 py-3 bg-secondary rounded-xl font-mono text-sm">
                  <span className="px-2 py-0.5 bg-blue-500/20 text-blue-700 dark:text-blue-400 rounded text-xs font-semibold">GET</span>
                  <span className="text-foreground">/api/v1/whmcs/status/{'{service_id}'}</span>
                </div>
              </div>
              <p className="text-xs text-muted-foreground mt-4">
                Include the API key in the <code className="px-1.5 py-0.5 bg-secondary rounded text-foreground">X-API-Key</code> header.
              </p>
            </div>
          </div>
        </div>
      )}

      {/* Delete Confirmation Modal */}
      <ConfirmModal
        isOpen={!!deleteConfirm}
        title="Delete API Key"
        message={`Are you sure you want to delete "${deleteConfirm?.name}"? Any integrations using this key will stop working.`}
        confirmLabel="Delete Key"
        isLoading={isDeleting}
        onConfirm={handleDeleteKey}
        onCancel={() => setDeleteConfirm(null)}
      />

      {/* Create Key Modal */}
      {showCreateModal && (
        <div className="fixed inset-0 bg-black/60 backdrop-blur-sm flex items-center justify-center z-50 p-4">
          <div className="bg-card border border-border rounded-2xl w-full max-w-md shadow-xl">
            <div className="flex items-center justify-between px-6 py-4 border-b border-border">
              <h3 className="font-semibold text-lg">Create API Key</h3>
              <button
                onClick={() => {
                  setShowCreateModal(false)
                  setNewKeyName('')
                  setNewKey(null)
                }}
                className="p-2 hover:bg-secondary rounded-lg transition-colors"
              >
                <X className="w-5 h-5" />
              </button>
            </div>

            {newKey ? (
              <div className="p-6 space-y-4">
                <div className="bg-emerald-500/10 border border-emerald-500/20 rounded-xl p-4">
                  <div className="flex items-start gap-3">
                    <Check className="w-5 h-5 text-emerald-600 dark:text-emerald-400 mt-0.5" />
                    <p className="text-sm text-emerald-700 dark:text-emerald-400">
                      API key created! Copy it now - you won't see it again.
                    </p>
                  </div>
                </div>

                <div className="space-y-2">
                  <label className="text-sm font-medium">Your API Key</label>
                  <div className="flex items-center gap-2">
                    <input
                      type="text"
                      readOnly
                      value={newKey.key}
                      className="flex-1 px-4 py-3 bg-secondary border border-border rounded-xl font-mono text-sm"
                    />
                    <button
                      onClick={() => copyToClipboard(newKey.key, 'new')}
                      className="p-3 bg-secondary hover:bg-secondary/80 rounded-xl transition-colors"
                    >
                      {copiedKey === 'new' ? (
                        <Check className="w-5 h-5 text-emerald-500" />
                      ) : (
                        <Copy className="w-5 h-5" />
                      )}
                    </button>
                  </div>
                </div>

                <button
                  onClick={() => {
                    setShowCreateModal(false)
                    setNewKey(null)
                  }}
                  className="w-full py-3 bg-secondary hover:bg-secondary/80 rounded-xl font-medium transition-colors"
                >
                  Done
                </button>
              </div>
            ) : (
              <div className="p-6 space-y-4">
                <div className="space-y-2">
                  <label className="text-sm font-medium">Key Name</label>
                  <input
                    type="text"
                    value={newKeyName}
                    onChange={(e) => setNewKeyName(e.target.value)}
                    placeholder="e.g., WHMCS Production"
                    className="w-full px-4 py-3 bg-secondary/50 border border-border rounded-xl focus:outline-none focus:ring-2 focus:ring-primary/50 focus:border-primary/50 transition-all"
                  />
                </div>

                <div className="flex gap-3 pt-2">
                  <button
                    onClick={() => {
                      setShowCreateModal(false)
                      setNewKeyName('')
                    }}
                    className="flex-1 py-3 bg-secondary hover:bg-secondary/80 rounded-xl font-medium transition-colors"
                  >
                    Cancel
                  </button>
                  <button
                    onClick={handleCreateKey}
                    disabled={isCreating || !newKeyName.trim()}
                    className="flex-1 py-3 bg-primary hover:bg-primary/90 text-primary-foreground rounded-xl font-medium transition-colors disabled:opacity-50 disabled:cursor-not-allowed flex items-center justify-center gap-2"
                  >
                    {isCreating ? (
                      <>
                        <Loader2 className="w-4 h-4 animate-spin" />
                        Creating...
                      </>
                    ) : (
                      'Create Key'
                    )}
                  </button>
                </div>
              </div>
            )}
          </div>
        </div>
      )}

      {/* Password Change Modal */}
      {showPasswordModal && (
        <div className="fixed inset-0 bg-black/60 backdrop-blur-sm flex items-center justify-center z-50 p-4">
          <div className="bg-card border border-border rounded-2xl w-full max-w-md shadow-xl">
            <div className="flex items-center justify-between px-6 py-4 border-b border-border">
              <h3 className="font-semibold text-lg">Change Password</h3>
              <button
                onClick={() => {
                  setShowPasswordModal(false)
                  setCurrentPassword('')
                  setNewPassword('')
                  setConfirmPassword('')
                  setPasswordError('')
                  setPasswordSuccess('')
                }}
                className="p-2 hover:bg-secondary rounded-lg transition-colors"
              >
                <X className="w-5 h-5" />
              </button>
            </div>

            <div className="p-6 space-y-4">
              {passwordError && (
                <div className="flex items-center gap-2 px-4 py-3 bg-red-500/10 border border-red-500/20 text-red-500 rounded-xl text-sm">
                  <AlertCircle className="w-4 h-4 flex-shrink-0" />
                  {passwordError}
                </div>
              )}
              {passwordSuccess && (
                <div className="flex items-center gap-2 px-4 py-3 bg-emerald-500/10 border border-emerald-500/20 text-emerald-500 rounded-xl text-sm">
                  <Check className="w-4 h-4 flex-shrink-0" />
                  {passwordSuccess}
                </div>
              )}

              <div className="space-y-2">
                <label className="text-sm font-medium">Current Password</label>
                <div className="relative">
                  <input
                    type={showCurrentPassword ? 'text' : 'password'}
                    value={currentPassword}
                    onChange={(e) => setCurrentPassword(e.target.value)}
                    className="w-full px-4 py-3 pr-12 bg-secondary/50 border border-border rounded-xl focus:outline-none focus:ring-2 focus:ring-primary/50 focus:border-primary/50 transition-all"
                  />
                  <button
                    type="button"
                    onClick={() => setShowCurrentPassword(!showCurrentPassword)}
                    className="absolute right-3 top-1/2 -translate-y-1/2 p-1 hover:bg-secondary rounded transition-colors"
                  >
                    {showCurrentPassword ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                  </button>
                </div>
              </div>

              <div className="space-y-2">
                <label className="text-sm font-medium">New Password</label>
                <div className="relative">
                  <input
                    type={showNewPassword ? 'text' : 'password'}
                    value={newPassword}
                    onChange={(e) => setNewPassword(e.target.value)}
                    className="w-full px-4 py-3 pr-12 bg-secondary/50 border border-border rounded-xl focus:outline-none focus:ring-2 focus:ring-primary/50 focus:border-primary/50 transition-all"
                  />
                  <button
                    type="button"
                    onClick={() => setShowNewPassword(!showNewPassword)}
                    className="absolute right-3 top-1/2 -translate-y-1/2 p-1 hover:bg-secondary rounded transition-colors"
                  >
                    {showNewPassword ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                  </button>
                </div>
              </div>

              <div className="space-y-2">
                <label className="text-sm font-medium">Confirm New Password</label>
                <input
                  type="password"
                  value={confirmPassword}
                  onChange={(e) => setConfirmPassword(e.target.value)}
                  className="w-full px-4 py-3 bg-secondary/50 border border-border rounded-xl focus:outline-none focus:ring-2 focus:ring-primary/50 focus:border-primary/50 transition-all"
                />
              </div>

              <div className="flex gap-3 pt-2">
                <button
                  onClick={() => {
                    setShowPasswordModal(false)
                    setCurrentPassword('')
                    setNewPassword('')
                    setConfirmPassword('')
                    setPasswordError('')
                  }}
                  className="flex-1 py-3 bg-secondary hover:bg-secondary/80 rounded-xl font-medium transition-colors"
                >
                  Cancel
                </button>
                <button
                  onClick={handleChangePassword}
                  disabled={isChangingPassword || !currentPassword || !newPassword || !confirmPassword}
                  className="flex-1 py-3 bg-primary hover:bg-primary/90 text-primary-foreground rounded-xl font-medium transition-colors disabled:opacity-50 disabled:cursor-not-allowed flex items-center justify-center gap-2"
                >
                  {isChangingPassword ? (
                    <>
                      <Loader2 className="w-4 h-4 animate-spin" />
                      Changing...
                    </>
                  ) : (
                    'Change Password'
                  )}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Add SSH Key Modal */}
      {showSSHKeyModal && (
        <div className="fixed inset-0 bg-black/60 backdrop-blur-sm flex items-center justify-center z-50 p-4">
          <div className="bg-card border border-border rounded-2xl w-full max-w-lg shadow-xl">
            <div className="flex items-center justify-between px-6 py-4 border-b border-border">
              <h3 className="font-semibold text-lg">Add SSH Key</h3>
              <button
                onClick={() => {
                  setShowSSHKeyModal(false)
                  setSSHKeyName('')
                  setSSHKeyPublic('')
                  setSSHKeyError('')
                }}
                className="p-2 hover:bg-secondary rounded-lg transition-colors"
              >
                <X className="w-5 h-5" />
              </button>
            </div>

            <div className="p-6 space-y-4">
              {sshKeyError && (
                <div className="flex items-center gap-2 px-4 py-3 bg-red-500/10 border border-red-500/20 text-red-500 rounded-xl text-sm">
                  <AlertCircle className="w-4 h-4 flex-shrink-0" />
                  {sshKeyError}
                </div>
              )}

              <div className="space-y-2">
                <label className="text-sm font-medium">Key Name</label>
                <input
                  type="text"
                  value={sshKeyName}
                  onChange={(e) => setSSHKeyName(e.target.value)}
                  placeholder="e.g., My MacBook"
                  className="w-full px-4 py-3 bg-secondary/50 border border-border rounded-xl focus:outline-none focus:ring-2 focus:ring-primary/50 focus:border-primary/50 transition-all"
                />
              </div>

              <div className="space-y-2">
                <label className="text-sm font-medium">Public Key</label>
                <textarea
                  value={sshKeyPublic}
                  onChange={(e) => setSSHKeyPublic(e.target.value)}
                  placeholder="ssh-ed25519 AAAA... or ssh-rsa AAAA..."
                  rows={4}
                  className="w-full px-4 py-3 bg-secondary/50 border border-border rounded-xl focus:outline-none focus:ring-2 focus:ring-primary/50 focus:border-primary/50 transition-all font-mono text-sm resize-none"
                />
                <p className="text-xs text-muted-foreground">
                  Paste your public key (id_ed25519.pub or id_rsa.pub)
                </p>
              </div>

              <div className="flex gap-3 pt-2">
                <button
                  onClick={() => {
                    setShowSSHKeyModal(false)
                    setSSHKeyName('')
                    setSSHKeyPublic('')
                    setSSHKeyError('')
                  }}
                  className="flex-1 py-3 bg-secondary hover:bg-secondary/80 rounded-xl font-medium transition-colors"
                >
                  Cancel
                </button>
                <button
                  onClick={handleAddSSHKey}
                  disabled={isAddingSSHKey || !sshKeyName.trim() || !sshKeyPublic.trim()}
                  className="flex-1 py-3 bg-primary hover:bg-primary/90 text-primary-foreground rounded-xl font-medium transition-colors disabled:opacity-50 disabled:cursor-not-allowed flex items-center justify-center gap-2"
                >
                  {isAddingSSHKey ? (
                    <>
                      <Loader2 className="w-4 h-4 animate-spin" />
                      Adding...
                    </>
                  ) : (
                    <>
                      <Upload className="w-4 h-4" />
                      Add Key
                    </>
                  )}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Delete SSH Key Confirmation Modal */}
      <ConfirmModal
        isOpen={!!deleteSSHKeyConfirm}
        title="Delete SSH Key"
        message={`Are you sure you want to delete "${deleteSSHKeyConfirm?.name || 'this key'}"? You won't be able to authenticate with this key anymore.`}
        confirmLabel="Delete Key"
        isLoading={isDeletingSSHKey}
        onConfirm={handleDeleteSSHKey}
        onCancel={() => setDeleteSSHKeyConfirm(null)}
      />
    </div>
  )
}
