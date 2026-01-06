import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Plus,
  User,
  Shield,
  Trash2,
  Edit,
  MoreVertical,
  Users,
  Cpu,
  HardDrive,
  Gauge,
  Eye,
  Wrench,
  X,
  Loader2,
  AlertCircle,
} from 'lucide-react'
import { api, FastCPUser, CreateUserRequest } from '@/lib/api'
import { useAuth } from '@/hooks/useAuth'
import { cn } from '@/lib/utils'

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
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
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

export function UsersPage() {
  const navigate = useNavigate()
  const { impersonate, user: currentUser } = useAuth()
  const [users, setUsers] = useState<FastCPUser[]>([])
  const [loading, setLoading] = useState(true)
  const [showCreateModal, setShowCreateModal] = useState(false)
  const [showEditModal, setShowEditModal] = useState(false)
  const [selectedUser, setSelectedUser] = useState<FastCPUser | null>(null)
  const [error, setError] = useState('')
  const [creating, setCreating] = useState(false)
  const [deleteConfirm, setDeleteConfirm] = useState<FastCPUser | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [openMenu, setOpenMenu] = useState<string | null>(null)

  const [form, setForm] = useState<CreateUserRequest>({
    username: '',
    password: '',
    is_admin: false,
    shell_access: false,
    site_limit: 0,
    ram_limit_mb: 0,
    cpu_percent: 0,
    max_processes: 0,
  })

  const fetchUsers = async () => {
    try {
      const response = await api.getUsers()
      setUsers(response.users || [])
    } catch (err) {
      console.error('Failed to fetch users:', err)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchUsers()
  }, [])

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setCreating(true)

    try {
      await api.createUser(form)
      setShowCreateModal(false)
      setForm({
        username: '',
        password: '',
        is_admin: false,
        shell_access: false,
        site_limit: 0,
        ram_limit_mb: 0,
        cpu_percent: 0,
        max_processes: 0,
      })
      fetchUsers()
    } catch (err: any) {
      setError(err.message || 'Failed to create user')
    } finally {
      setCreating(false)
    }
  }

  const handleUpdate = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!selectedUser) return
    setError('')
    setCreating(true)

    try {
      await api.updateUser(selectedUser.username, {
        password: form.password || undefined,
        enabled: true,
        shell_access: form.shell_access,
        site_limit: form.site_limit,
        ram_limit_mb: form.ram_limit_mb,
        cpu_percent: form.cpu_percent,
        max_processes: form.max_processes,
      })
      setShowEditModal(false)
      setSelectedUser(null)
      fetchUsers()
    } catch (err: any) {
      setError(err.message || 'Failed to update user')
    } finally {
      setCreating(false)
    }
  }

  const handleDelete = async () => {
    if (!deleteConfirm) return
    setDeleting(true)

    try {
      await api.deleteUser(deleteConfirm.username)
      setDeleteConfirm(null)
      fetchUsers()
    } catch (err: any) {
      alert(err.message || 'Failed to delete user')
    } finally {
      setDeleting(false)
    }
  }

  const handleToggleEnabled = async (user: FastCPUser) => {
    try {
      await api.updateUser(user.username, {
        enabled: !user.enabled,
        site_limit: user.site_limit,
        ram_limit_mb: user.ram_limit_mb,
        cpu_percent: user.cpu_percent,
        max_processes: user.max_processes,
      })
      fetchUsers()
    } catch (err: any) {
      alert(err.message || 'Failed to update user')
    }
    setOpenMenu(null)
  }

  const openEditModal = (user: FastCPUser) => {
    setSelectedUser(user)
    setForm({
      username: user.username,
      password: '',
      is_admin: user.is_admin,
      shell_access: user.shell_access,
      site_limit: user.site_limit,
      ram_limit_mb: user.ram_limit_mb,
      cpu_percent: user.cpu_percent,
      max_processes: user.max_processes,
    })
    setShowEditModal(true)
    setError('')
    setOpenMenu(null)
  }

  const handleImpersonate = (username: string) => {
    impersonate(username)
    navigate('/')
  }

  const handleFixPermissions = async () => {
    if (!confirm('This will fix SSH access and directory permissions for all users. Continue?')) {
      return
    }

    try {
      const result = await api.fixUserPermissions()
      alert(`Fixed permissions for ${result.users_fixed} users. Errors: ${result.errors}`)
      fetchUsers()
    } catch (err: any) {
      alert(err.message || 'Failed to fix permissions')
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="w-10 h-10 border-2 border-primary border-t-transparent rounded-full animate-spin" />
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Users</h1>
          <p className="text-muted-foreground mt-1">
            Manage Unix users and their resource limits
          </p>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={handleFixPermissions}
            className="flex items-center gap-2 px-4 py-2.5 bg-secondary hover:bg-secondary/80 rounded-xl font-medium transition-colors"
            title="Fix SSH and directory permissions for all users"
          >
            <Wrench className="w-4 h-4" />
            Fix Permissions
          </button>
          <button
            onClick={() => {
              setShowCreateModal(true)
              setError('')
              setForm({
                username: '',
                password: '',
                is_admin: false,
                shell_access: false,
                site_limit: 0,
                ram_limit_mb: 0,
                cpu_percent: 0,
                max_processes: 0,
              })
            }}
            className="flex items-center gap-2 px-4 py-2.5 bg-primary hover:bg-primary/90 text-primary-foreground rounded-xl font-medium transition-colors"
          >
            <Plus className="w-4 h-4" />
            Create User
          </button>
        </div>
      </div>

      {/* Users Grid */}
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        {users.map((user) => (
          <div
            key={user.username}
            className="bg-card border border-border rounded-2xl p-5 space-y-4 card-shadow"
          >
            {/* User Header */}
            <div className="flex items-start justify-between">
              <div className="flex items-center gap-3">
                <div className={cn(
                  "w-10 h-10 rounded-xl flex items-center justify-center",
                  user.is_admin 
                    ? "bg-amber-500/10" 
                    : "bg-primary/10"
                )}>
                  {user.is_admin ? (
                    <Shield className="w-5 h-5 text-amber-600 dark:text-amber-400" />
                  ) : (
                    <User className="w-5 h-5 text-primary" />
                  )}
                </div>
                <div>
                  <h3 className="font-semibold">{user.username}</h3>
                  <div className="flex items-center gap-2 flex-wrap mt-1">
                    <span className={cn(
                      "text-xs px-2 py-0.5 rounded-full",
                      user.is_admin 
                        ? "bg-amber-500/10 text-amber-700 dark:text-amber-400" 
                        : "bg-secondary text-muted-foreground"
                    )}>
                      {user.is_admin ? 'Admin' : 'User'}
                    </span>
                    <span className={cn(
                      "text-xs px-2 py-0.5 rounded-full",
                      user.enabled 
                        ? "bg-emerald-500/10 text-emerald-700 dark:text-emerald-400" 
                        : "bg-red-500/10 text-red-700 dark:text-red-400"
                    )}>
                      {user.enabled ? 'Active' : 'Disabled'}
                    </span>
                  </div>
                </div>
              </div>
              <div className="relative">
                <button
                  onClick={() => setOpenMenu(openMenu === user.username ? null : user.username)}
                  className="p-1.5 text-muted-foreground hover:text-foreground rounded-lg hover:bg-secondary transition-colors"
                >
                  <MoreVertical className="w-4 h-4" />
                </button>
                {openMenu === user.username && (
                  <>
                    <div className="fixed inset-0 z-10" onClick={() => setOpenMenu(null)} />
                    <div className="absolute right-0 top-8 bg-card border border-border rounded-xl shadow-lg z-20 min-w-[160px] overflow-hidden">
                      {user.username !== currentUser?.username && !user.is_admin && (
                        <button
                          onClick={() => {
                            handleImpersonate(user.username)
                            setOpenMenu(null)
                          }}
                          className="w-full flex items-center gap-2 px-3 py-2 text-sm hover:bg-secondary text-left text-amber-600 dark:text-amber-400"
                        >
                          <Eye className="w-4 h-4" /> View as User
                        </button>
                      )}
                      <button
                        onClick={() => openEditModal(user)}
                        className="w-full flex items-center gap-2 px-3 py-2 text-sm hover:bg-secondary text-left"
                      >
                        <Edit className="w-4 h-4" /> Edit
                      </button>
                      <button
                        onClick={() => handleToggleEnabled(user)}
                        className="w-full flex items-center gap-2 px-3 py-2 text-sm hover:bg-secondary text-left"
                      >
                        {user.enabled ? 'Disable' : 'Enable'}
                      </button>
                      {user.username !== 'root' && user.username !== currentUser?.username && (
                        <>
                          <div className="border-t border-border" />
                          <button
                            onClick={() => {
                              setDeleteConfirm(user)
                              setOpenMenu(null)
                            }}
                            className="w-full flex items-center gap-2 px-3 py-2 text-sm hover:bg-red-500/10 text-red-500 text-left"
                          >
                            <Trash2 className="w-4 h-4" /> Delete
                          </button>
                        </>
                      )}
                    </div>
                  </>
                )}
              </div>
            </div>

            {/* Stats */}
            <div className="grid grid-cols-2 gap-3">
              <div className="bg-secondary/50 rounded-xl p-3">
                <div className="flex items-center gap-2 text-muted-foreground text-xs mb-1">
                  <Users className="w-3 h-3" /> Sites
                </div>
                <p className="font-semibold">
                  {user.site_count}
                  {user.site_limit > 0 && (
                    <span className="text-muted-foreground font-normal"> / {user.site_limit}</span>
                  )}
                </p>
              </div>
              <div className="bg-secondary/50 rounded-xl p-3">
                <div className="flex items-center gap-2 text-muted-foreground text-xs mb-1">
                  <HardDrive className="w-3 h-3" /> Disk
                </div>
                <p className="font-semibold">{user.disk_used_mb} MB</p>
              </div>
              <div className="bg-secondary/50 rounded-xl p-3">
                <div className="flex items-center gap-2 text-muted-foreground text-xs mb-1">
                  <Gauge className="w-3 h-3" /> RAM
                </div>
                <p className="font-semibold">
                  {user.ram_used_mb} MB
                  {user.ram_limit_mb > 0 && (
                    <span className="text-muted-foreground font-normal"> / {user.ram_limit_mb}</span>
                  )}
                </p>
              </div>
              <div className="bg-secondary/50 rounded-xl p-3">
                <div className="flex items-center gap-2 text-muted-foreground text-xs mb-1">
                  <Cpu className="w-3 h-3" /> Processes
                </div>
                <p className="font-semibold">
                  {user.process_count}
                  {user.max_processes > 0 && (
                    <span className="text-muted-foreground font-normal"> / {user.max_processes}</span>
                  )}
                </p>
              </div>
            </div>
          </div>
        ))}
      </div>

      {users.length === 0 && (
        <div className="bg-card border border-border rounded-2xl p-12 text-center card-shadow">
          <div className="w-16 h-16 rounded-2xl bg-secondary flex items-center justify-center mx-auto mb-4">
            <Users className="w-8 h-8 text-muted-foreground" />
          </div>
          <h3 className="text-lg font-semibold mb-2">No users yet</h3>
          <p className="text-muted-foreground mb-4">
            Create your first user to get started
          </p>
          <button
            onClick={() => setShowCreateModal(true)}
            className="px-4 py-2.5 bg-primary hover:bg-primary/90 text-primary-foreground rounded-xl font-medium transition-colors"
          >
            Create User
          </button>
        </div>
      )}

      {/* Delete Confirmation */}
      <ConfirmModal
        isOpen={!!deleteConfirm}
        title="Delete User"
        message={`Are you sure you want to delete "${deleteConfirm?.username}"? This action cannot be undone.`}
        confirmLabel="Delete User"
        isLoading={deleting}
        onConfirm={handleDelete}
        onCancel={() => setDeleteConfirm(null)}
      />

      {/* Create Modal */}
      {showCreateModal && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
          <div className="bg-card border border-border rounded-2xl w-full max-w-md max-h-[90vh] overflow-y-auto shadow-xl">
            <div className="flex items-center justify-between px-6 py-4 border-b border-border">
              <div>
                <h2 className="text-lg font-semibold">Create User</h2>
                <p className="text-sm text-muted-foreground">
                  Create a new Unix user with resource limits
                </p>
              </div>
              <button
                onClick={() => setShowCreateModal(false)}
                className="p-2 hover:bg-secondary rounded-lg transition-colors"
              >
                <X className="w-5 h-5" />
              </button>
            </div>
            <form onSubmit={handleCreate} className="p-6 space-y-4">
              {error && (
                <div className="p-3 bg-red-500/10 border border-red-500/20 rounded-xl text-red-600 dark:text-red-400 text-sm">
                  {error}
                </div>
              )}

              <div>
                <label className="block text-sm font-medium mb-2">Username</label>
                <input
                  type="text"
                  value={form.username}
                  onChange={(e) => setForm({ ...form, username: e.target.value })}
                  className="w-full px-4 py-3 bg-secondary/50 border border-border rounded-xl focus:outline-none focus:ring-2 focus:ring-primary/50 focus:border-primary transition-colors"
                  placeholder="john"
                  required
                />
              </div>

              <div>
                <label className="block text-sm font-medium mb-2">Password</label>
                <input
                  type="password"
                  value={form.password}
                  onChange={(e) => setForm({ ...form, password: e.target.value })}
                  className="w-full px-4 py-3 bg-secondary/50 border border-border rounded-xl focus:outline-none focus:ring-2 focus:ring-primary/50 focus:border-primary transition-colors"
                  placeholder="••••••••"
                  required
                  minLength={8}
                />
                <p className="text-xs text-muted-foreground mt-1">Minimum 8 characters</p>
              </div>

              <div className="space-y-3">
                <div className="flex items-center gap-3">
                  <input
                    type="checkbox"
                    id="is_admin"
                    checked={form.is_admin}
                    onChange={(e) => setForm({ ...form, is_admin: e.target.checked, shell_access: e.target.checked || form.shell_access })}
                    className="w-4 h-4 rounded border-border bg-secondary text-primary focus:ring-primary"
                  />
                  <label htmlFor="is_admin" className="text-sm">
                    Grant admin privileges (sudo access)
                  </label>
                </div>

                <div className="flex items-center gap-3">
                  <input
                    type="checkbox"
                    id="shell_access"
                    checked={form.shell_access || form.is_admin}
                    disabled={form.is_admin}
                    onChange={(e) => setForm({ ...form, shell_access: e.target.checked })}
                    className="w-4 h-4 rounded border-border bg-secondary text-primary focus:ring-primary disabled:opacity-50"
                  />
                  <div>
                    <label htmlFor="shell_access" className="text-sm">
                      Allow SSH shell access
                    </label>
                    <p className="text-xs text-muted-foreground">
                      {form.shell_access || form.is_admin 
                        ? "User can SSH with full shell access" 
                        : "SFTP only - user is jailed to their home directory"}
                    </p>
                  </div>
                </div>
              </div>

              <div className="border-t border-border pt-4">
                <h3 className="font-medium mb-3">Resource Limits</h3>
                <p className="text-xs text-muted-foreground mb-4">Set to 0 for unlimited</p>

                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <label className="block text-sm font-medium mb-2">Max Sites</label>
                    <input
                      type="number"
                      value={form.site_limit}
                      onChange={(e) => setForm({ ...form, site_limit: parseInt(e.target.value) || 0 })}
                      className="w-full px-4 py-2.5 bg-secondary/50 border border-border rounded-xl focus:outline-none focus:ring-2 focus:ring-primary/50 transition-colors"
                      min={0}
                    />
                  </div>
                  <div>
                    <label className="block text-sm font-medium mb-2">Max RAM (MB)</label>
                    <input
                      type="number"
                      value={form.ram_limit_mb}
                      onChange={(e) => setForm({ ...form, ram_limit_mb: parseInt(e.target.value) || 0 })}
                      className="w-full px-4 py-2.5 bg-secondary/50 border border-border rounded-xl focus:outline-none focus:ring-2 focus:ring-primary/50 transition-colors"
                      min={0}
                    />
                  </div>
                  <div>
                    <label className="block text-sm font-medium mb-2">CPU %</label>
                    <input
                      type="number"
                      value={form.cpu_percent}
                      onChange={(e) => setForm({ ...form, cpu_percent: parseInt(e.target.value) || 0 })}
                      className="w-full px-4 py-2.5 bg-secondary/50 border border-border rounded-xl focus:outline-none focus:ring-2 focus:ring-primary/50 transition-colors"
                      min={0}
                    />
                  </div>
                  <div>
                    <label className="block text-sm font-medium mb-2">Max Processes</label>
                    <input
                      type="number"
                      value={form.max_processes}
                      onChange={(e) => setForm({ ...form, max_processes: parseInt(e.target.value) || 0 })}
                      className="w-full px-4 py-2.5 bg-secondary/50 border border-border rounded-xl focus:outline-none focus:ring-2 focus:ring-primary/50 transition-colors"
                      min={0}
                    />
                  </div>
                </div>
              </div>

              <div className="flex gap-3 pt-4">
                <button
                  type="button"
                  onClick={() => setShowCreateModal(false)}
                  className="flex-1 px-4 py-2.5 bg-secondary hover:bg-secondary/80 rounded-xl font-medium transition-colors"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={creating}
                  className="flex-1 flex items-center justify-center gap-2 px-4 py-2.5 bg-primary hover:bg-primary/90 text-primary-foreground rounded-xl font-medium transition-colors disabled:opacity-50"
                >
                  {creating ? (
                    <>
                      <Loader2 className="w-4 h-4 animate-spin" />
                      Creating...
                    </>
                  ) : (
                    'Create User'
                  )}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* Edit Modal */}
      {showEditModal && selectedUser && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
          <div className="bg-card border border-border rounded-2xl w-full max-w-md max-h-[90vh] overflow-y-auto shadow-xl">
            <div className="flex items-center justify-between px-6 py-4 border-b border-border">
              <div>
                <h2 className="text-lg font-semibold">Edit User: {selectedUser.username}</h2>
                <p className="text-sm text-muted-foreground">
                  Update password and resource limits
                </p>
              </div>
              <button
                onClick={() => {
                  setShowEditModal(false)
                  setSelectedUser(null)
                }}
                className="p-2 hover:bg-secondary rounded-lg transition-colors"
              >
                <X className="w-5 h-5" />
              </button>
            </div>
            <form onSubmit={handleUpdate} className="p-6 space-y-4">
              {error && (
                <div className="p-3 bg-red-500/10 border border-red-500/20 rounded-xl text-red-600 dark:text-red-400 text-sm">
                  {error}
                </div>
              )}

              <div>
                <label className="block text-sm font-medium mb-2">New Password (optional)</label>
                <input
                  type="password"
                  value={form.password}
                  onChange={(e) => setForm({ ...form, password: e.target.value })}
                  className="w-full px-4 py-3 bg-secondary/50 border border-border rounded-xl focus:outline-none focus:ring-2 focus:ring-primary/50 transition-colors"
                  placeholder="Leave blank to keep current"
                  minLength={8}
                />
              </div>

              {!selectedUser?.is_admin && (
                <div className="flex items-center gap-3 p-3 bg-secondary/50 rounded-xl">
                  <input
                    type="checkbox"
                    id="edit_shell_access"
                    checked={form.shell_access}
                    onChange={(e) => setForm({ ...form, shell_access: e.target.checked })}
                    className="w-4 h-4 rounded border-border bg-secondary text-primary focus:ring-primary"
                  />
                  <div>
                    <label htmlFor="edit_shell_access" className="text-sm font-medium">
                      Allow SSH shell access
                    </label>
                    <p className="text-xs text-muted-foreground">
                      {form.shell_access 
                        ? "User can SSH with full shell access" 
                        : "SFTP only - user is jailed to their home directory"}
                    </p>
                  </div>
                </div>
              )}

              <div className="border-t border-border pt-4">
                <h3 className="font-medium mb-3">Resource Limits</h3>
                <p className="text-xs text-muted-foreground mb-4">Set to 0 for unlimited</p>

                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <label className="block text-sm font-medium mb-2">Max Sites</label>
                    <input
                      type="number"
                      value={form.site_limit}
                      onChange={(e) => setForm({ ...form, site_limit: parseInt(e.target.value) || 0 })}
                      className="w-full px-4 py-2.5 bg-secondary/50 border border-border rounded-xl focus:outline-none focus:ring-2 focus:ring-primary/50 transition-colors"
                      min={0}
                    />
                  </div>
                  <div>
                    <label className="block text-sm font-medium mb-2">Max RAM (MB)</label>
                    <input
                      type="number"
                      value={form.ram_limit_mb}
                      onChange={(e) => setForm({ ...form, ram_limit_mb: parseInt(e.target.value) || 0 })}
                      className="w-full px-4 py-2.5 bg-secondary/50 border border-border rounded-xl focus:outline-none focus:ring-2 focus:ring-primary/50 transition-colors"
                      min={0}
                    />
                  </div>
                  <div>
                    <label className="block text-sm font-medium mb-2">CPU %</label>
                    <input
                      type="number"
                      value={form.cpu_percent}
                      onChange={(e) => setForm({ ...form, cpu_percent: parseInt(e.target.value) || 0 })}
                      className="w-full px-4 py-2.5 bg-secondary/50 border border-border rounded-xl focus:outline-none focus:ring-2 focus:ring-primary/50 transition-colors"
                      min={0}
                    />
                  </div>
                  <div>
                    <label className="block text-sm font-medium mb-2">Max Processes</label>
                    <input
                      type="number"
                      value={form.max_processes}
                      onChange={(e) => setForm({ ...form, max_processes: parseInt(e.target.value) || 0 })}
                      className="w-full px-4 py-2.5 bg-secondary/50 border border-border rounded-xl focus:outline-none focus:ring-2 focus:ring-primary/50 transition-colors"
                      min={0}
                    />
                  </div>
                </div>
              </div>

              <div className="flex gap-3 pt-4">
                <button
                  type="button"
                  onClick={() => {
                    setShowEditModal(false)
                    setSelectedUser(null)
                  }}
                  className="flex-1 px-4 py-2.5 bg-secondary hover:bg-secondary/80 rounded-xl font-medium transition-colors"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={creating}
                  className="flex-1 flex items-center justify-center gap-2 px-4 py-2.5 bg-primary hover:bg-primary/90 text-primary-foreground rounded-xl font-medium transition-colors disabled:opacity-50"
                >
                  {creating ? (
                    <>
                      <Loader2 className="w-4 h-4 animate-spin" />
                      Saving...
                    </>
                  ) : (
                    'Save Changes'
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
