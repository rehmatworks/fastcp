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
} from 'lucide-react'
import { api } from '@/lib/api'
import { formatDate } from '@/lib/utils'
import type { APIKey } from '@/types'

export function SettingsPage() {
  const [apiKeys, setAPIKeys] = useState<APIKey[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [showCreateModal, setShowCreateModal] = useState(false)
  const [newKey, setNewKey] = useState<APIKey | null>(null)
  const [newKeyName, setNewKeyName] = useState('')
  const [isCreating, setIsCreating] = useState(false)
  const [copiedKey, setCopiedKey] = useState<string | null>(null)
  const [isReloading, setIsReloading] = useState(false)

  useEffect(() => {
    fetchAPIKeys()
  }, [])

  async function fetchAPIKeys() {
    try {
      const data = await api.getAPIKeys()
      setAPIKeys(data.api_keys || [])
    } catch (error) {
      console.error('Failed to fetch API keys:', error)
    } finally {
      setIsLoading(false)
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

  async function handleDeleteKey(id: string) {
    if (!confirm('Are you sure you want to delete this API key?')) return

    try {
      await api.deleteAPIKey(id)
      fetchAPIKeys()
    } catch (error) {
      console.error('Failed to delete API key:', error)
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
        <div className="w-8 h-8 border-2 border-emerald-500 border-t-transparent rounded-full animate-spin" />
      </div>
    )
  }

  return (
    <div className="space-y-6 animate-fade-in">
      {/* Header */}
      <div>
        <h1 className="text-2xl font-bold">Settings</h1>
        <p className="text-muted-foreground">
          Manage API keys and system settings
        </p>
      </div>

      {/* Quick Actions */}
      <div className="bg-card border border-border rounded-xl p-5">
        <h2 className="font-semibold mb-4">Quick Actions</h2>
        <div className="flex flex-wrap gap-3">
          <button
            onClick={handleReloadAll}
            disabled={isReloading}
            className="flex items-center gap-2 px-4 py-2 bg-secondary hover:bg-secondary/80 rounded-lg transition-colors disabled:opacity-50"
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

      {/* API Keys */}
      <div className="bg-card border border-border rounded-xl">
        <div className="flex items-center justify-between px-5 py-4 border-b border-border">
          <div className="flex items-center gap-3">
            <Key className="w-5 h-5 text-emerald-400" />
            <div>
              <h2 className="font-semibold">API Keys</h2>
              <p className="text-sm text-muted-foreground">
                For WHMCS and external integrations
              </p>
            </div>
          </div>
          <button
            onClick={() => setShowCreateModal(true)}
            className="flex items-center gap-2 px-3 py-2 text-sm bg-emerald-500/20 text-emerald-400 hover:bg-emerald-500/30 rounded-lg transition-colors"
          >
            <Plus className="w-4 h-4" />
            Create Key
          </button>
        </div>

        <div className="divide-y divide-border">
          {apiKeys.length === 0 ? (
            <div className="px-5 py-8 text-center text-muted-foreground">
              <Key className="w-10 h-10 mx-auto mb-3 opacity-50" />
              <p>No API keys yet</p>
              <button
                onClick={() => setShowCreateModal(true)}
                className="text-sm text-emerald-400 hover:text-emerald-300 mt-2"
              >
                Create your first API key
              </button>
            </div>
          ) : (
            apiKeys.map((key) => (
              <div
                key={key.id}
                className="flex items-center justify-between px-5 py-4"
              >
                <div className="flex items-center gap-3">
                  <div className="w-9 h-9 rounded-lg bg-secondary flex items-center justify-center">
                    <Shield className="w-4 h-4 text-muted-foreground" />
                  </div>
                  <div>
                    <p className="font-medium">{key.name}</p>
                    <p className="text-sm text-muted-foreground font-mono">
                      {key.key}
                    </p>
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  <span className="text-xs text-muted-foreground">
                    {formatDate(key.created_at)}
                  </span>
                  <button
                    onClick={() => handleDeleteKey(key.id)}
                    className="p-2 text-muted-foreground hover:text-red-400 hover:bg-red-500/10 rounded-lg transition-colors"
                  >
                    <Trash2 className="w-4 h-4" />
                  </button>
                </div>
              </div>
            ))
          )}
        </div>
      </div>

      {/* WHMCS Integration Info */}
      <div className="bg-gradient-to-r from-blue-500/10 to-purple-500/10 border border-blue-500/20 rounded-xl p-5">
        <div className="flex items-start gap-4">
          <div className="w-10 h-10 rounded-lg bg-blue-500/20 flex items-center justify-center flex-shrink-0">
            <Shield className="w-5 h-5 text-blue-400" />
          </div>
          <div>
            <h3 className="font-semibold mb-1">WHMCS Integration</h3>
            <p className="text-sm text-muted-foreground mb-3">
              Use API keys to integrate FastCP with WHMCS for automated
              provisioning. Send requests to the following endpoints:
            </p>
            <div className="space-y-2 font-mono text-xs">
              <div className="bg-black/30 rounded px-3 py-2">
                <span className="text-emerald-400">POST</span>{' '}
                <span className="text-muted-foreground">/api/v1/whmcs/provision</span>
              </div>
              <div className="bg-black/30 rounded px-3 py-2">
                <span className="text-blue-400">GET</span>{' '}
                <span className="text-muted-foreground">/api/v1/whmcs/status/{'{service_id}'}</span>
              </div>
            </div>
          </div>
        </div>
      </div>

      {/* Create Key Modal */}
      {showCreateModal && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
          <div className="bg-card border border-border rounded-xl w-full max-w-md animate-slide-up">
            <div className="px-5 py-4 border-b border-border">
              <h3 className="font-semibold">Create API Key</h3>
            </div>

            {newKey ? (
              <div className="p-5 space-y-4">
                <div className="bg-emerald-500/10 border border-emerald-500/20 rounded-lg p-4">
                  <p className="text-sm text-emerald-400 mb-2">
                    API key created successfully! Copy it now - you won't be able to
                    see it again.
                  </p>
                </div>

                <div className="space-y-2">
                  <label className="text-sm font-medium">Your API Key</label>
                  <div className="flex items-center gap-2">
                    <input
                      type="text"
                      readOnly
                      value={newKey.key}
                      className="flex-1 px-4 py-2.5 bg-secondary border border-border rounded-lg font-mono text-sm"
                    />
                    <button
                      onClick={() => copyToClipboard(newKey.key, 'new')}
                      className="p-2.5 bg-secondary hover:bg-secondary/80 rounded-lg transition-colors"
                    >
                      {copiedKey === 'new' ? (
                        <Check className="w-4 h-4 text-emerald-400" />
                      ) : (
                        <Copy className="w-4 h-4" />
                      )}
                    </button>
                  </div>
                </div>

                <button
                  onClick={() => {
                    setShowCreateModal(false)
                    setNewKey(null)
                  }}
                  className="w-full py-2.5 bg-secondary hover:bg-secondary/80 rounded-lg transition-colors"
                >
                  Close
                </button>
              </div>
            ) : (
              <div className="p-5 space-y-4">
                <div className="space-y-2">
                  <label className="text-sm font-medium">Key Name</label>
                  <input
                    type="text"
                    value={newKeyName}
                    onChange={(e) => setNewKeyName(e.target.value)}
                    placeholder="e.g., WHMCS Production"
                    className="w-full px-4 py-2.5 bg-secondary border border-border rounded-lg focus:outline-none focus:ring-2 focus:ring-emerald-500/50 focus:border-emerald-500 transition-colors"
                  />
                </div>

                <div className="flex items-center gap-3">
                  <button
                    onClick={handleCreateKey}
                    disabled={isCreating || !newKeyName.trim()}
                    className="flex-1 py-2.5 bg-emerald-500 hover:bg-emerald-600 text-white rounded-lg transition-colors disabled:opacity-50 disabled:cursor-not-allowed flex items-center justify-center gap-2"
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
                  <button
                    onClick={() => {
                      setShowCreateModal(false)
                      setNewKeyName('')
                    }}
                    className="px-4 py-2.5 text-muted-foreground hover:text-foreground transition-colors"
                  >
                    Cancel
                  </button>
                </div>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

