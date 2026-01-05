import { Link, useLocation, useNavigate } from 'react-router-dom'
import {
  LayoutDashboard,
  Globe,
  Server,
  Settings,
  LogOut,
  Menu,
  X,
  Users,
  UserX,
  Database,
  Download,
  Loader2,
  CheckCircle,
  AlertCircle,
} from 'lucide-react'
import { useState, useEffect, useCallback, useRef } from 'react'
import { cn } from '@/lib/utils'
import { useAuth } from '@/hooks/useAuth'
import { api, VersionCheckResult, UpgradeStatus } from '@/lib/api'

const navigation = [
  { name: 'Dashboard', href: '/', icon: LayoutDashboard },
  { name: 'Sites', href: '/sites', icon: Globe },
  { name: 'Databases', href: '/databases', icon: Database },
  { name: 'PHP', href: '/php', icon: Server, adminOnly: true },
  { name: 'Users', href: '/users', icon: Users, adminOnly: true },
  { name: 'Settings', href: '/settings', icon: Settings, adminOnly: true },
]

export function Layout({ children }: { children: React.ReactNode }) {
  const location = useLocation()
  const navigate = useNavigate()
  const { user, realUser, logout, isImpersonating, stopImpersonating } = useAuth()
  const [sidebarOpen, setSidebarOpen] = useState(false)
  const [versionInfo, setVersionInfo] = useState<VersionCheckResult | null>(null)
  const [upgradeStatus, setUpgradeStatus] = useState<UpgradeStatus | null>(null)
  const [upgrading, setUpgrading] = useState(false)
  const [showUpgradeModal, setShowUpgradeModal] = useState(false)
  const pollIntervalRef = useRef<NodeJS.Timeout | null>(null)

  // When impersonating, use realUser for admin checks
  const effectiveRole = isImpersonating ? realUser?.role : user?.role

  // Check for updates on mount
  useEffect(() => {
    if (effectiveRole === 'admin') {
      checkForUpdates()
    }
  }, [effectiveRole])

  // Cleanup polling on unmount
  useEffect(() => {
    return () => {
      if (pollIntervalRef.current) {
        clearInterval(pollIntervalRef.current)
      }
    }
  }, [])

  const checkForUpdates = async () => {
    try {
      const result = await api.getVersion()
      setVersionInfo(result)
    } catch (error) {
      console.error('Failed to check for updates:', error)
    }
  }

  const pollUpgradeStatus = useCallback(async () => {
    try {
      const status = await api.getUpgradeStatus()
      setUpgradeStatus(status)

      if (!status.in_progress) {
        // Upgrade finished, stop polling
        if (pollIntervalRef.current) {
          clearInterval(pollIntervalRef.current)
          pollIntervalRef.current = null
        }
        setUpgrading(false)

        if (status.success) {
          // Reload page after successful upgrade
          setTimeout(() => {
            window.location.reload()
          }, 3000)
        }
      }
    } catch (error) {
      console.error('Failed to get upgrade status:', error)
    }
  }, [])

  const handleUpgrade = async () => {
    if (!confirm('Upgrade FastCP to the latest version? The control panel will restart automatically.')) {
      return
    }

    setUpgrading(true)
    setUpgradeStatus({ in_progress: true, success: false, message: 'Starting upgrade...' })

    try {
      await api.startUpgrade()
      // Start polling for status
      pollIntervalRef.current = setInterval(pollUpgradeStatus, 2000)
    } catch (error: any) {
      setUpgrading(false)
      setUpgradeStatus({
        in_progress: false,
        success: false,
        error: error.message || 'Failed to start upgrade',
      })
    }
  }

  const handleLogout = () => {
    logout()
    navigate('/login')
  }

  const handleStopImpersonating = () => {
    stopImpersonating()
    navigate('/')
  }

  return (
    <div className="min-h-screen bg-background">
      {/* Mobile sidebar backdrop */}
      {sidebarOpen && (
        <div
          className="fixed inset-0 bg-black/50 z-40 lg:hidden"
          onClick={() => setSidebarOpen(false)}
        />
      )}

      {/* Sidebar */}
      <aside
        className={cn(
          'fixed inset-y-0 left-0 z-50 w-64 bg-card border-r border-border transform transition-transform duration-200 ease-in-out lg:translate-x-0',
          sidebarOpen ? 'translate-x-0' : '-translate-x-full'
        )}
      >
        <div className="flex flex-col h-full">
          {/* Logo */}
          <div className="flex items-center gap-3 px-6 py-5 border-b border-border">
            <div className="w-9 h-9 rounded-lg bg-gradient-to-br from-emerald-500 to-emerald-600 flex items-center justify-center">
              <span className="text-white font-bold text-lg">F</span>
            </div>
            <div>
              <h1 className="font-semibold text-lg">FastCP</h1>
              <div className="flex items-center gap-2">
                <p className="text-xs text-muted-foreground">
                  {versionInfo ? `v${versionInfo.current_version}` : 'Control Panel'}
                </p>
                {versionInfo?.update_available && (
                  <span className="w-2 h-2 rounded-full bg-blue-500 animate-pulse" title="Update available" />
                )}
              </div>
            </div>
            <button
              className="ml-auto lg:hidden"
              onClick={() => setSidebarOpen(false)}
            >
              <X className="w-5 h-5" />
            </button>
          </div>

          {/* Navigation */}
          <nav className="flex-1 px-3 py-4 space-y-1">
            {navigation
              .filter((item) => !item.adminOnly || effectiveRole === 'admin')
              .map((item) => {
                const isActive = location.pathname === item.href || 
                  (item.href !== '/' && location.pathname.startsWith(item.href))
                return (
                  <Link
                    key={item.name}
                    to={item.href}
                    onClick={() => setSidebarOpen(false)}
                    className={cn(
                      'flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium transition-colors',
                      isActive
                        ? 'bg-emerald-500/10 text-emerald-400'
                        : 'text-muted-foreground hover:text-foreground hover:bg-secondary'
                    )}
                  >
                    <item.icon className="w-5 h-5" />
                    {item.name}
                  </Link>
                )
              })}
          </nav>

          {/* User section */}
          <div className="p-3 border-t border-border">
            <div className="flex items-center gap-3 px-3 py-2">
              <div className="w-8 h-8 rounded-full bg-gradient-to-br from-emerald-500 to-emerald-600 flex items-center justify-center">
                <span className="text-white text-sm font-medium">
                  {user?.username?.charAt(0).toUpperCase() || 'A'}
                </span>
              </div>
              <div className="flex-1 min-w-0">
                <p className="text-sm font-medium truncate">{user?.username}</p>
                <p className="text-xs text-muted-foreground">{user?.role}</p>
              </div>
              <button
                onClick={handleLogout}
                className="p-2 text-muted-foreground hover:text-foreground hover:bg-secondary rounded-lg transition-colors"
              >
                <LogOut className="w-4 h-4" />
              </button>
            </div>
          </div>
        </div>
      </aside>

      {/* Main content */}
      <div className="lg:pl-64">
        {/* Update available banner */}
        {versionInfo?.update_available && effectiveRole === 'admin' && !upgrading && (
          <div className="bg-blue-500/20 border-b border-blue-500/30 px-4 py-2">
            <div className="flex items-center justify-between max-w-7xl mx-auto">
              <div className="flex items-center gap-2 text-blue-200">
                <Download className="w-4 h-4" />
                <span className="text-sm">
                  Update available: <strong>{versionInfo.release_name}</strong>
                  <span className="text-blue-300/70 ml-2">(current: v{versionInfo.current_version})</span>
                </span>
              </div>
              <div className="flex items-center gap-2">
                <button
                  onClick={() => setShowUpgradeModal(true)}
                  className="text-xs px-3 py-1 bg-blue-500/30 hover:bg-blue-500/50 rounded-lg transition-colors text-blue-100"
                >
                  View Details
                </button>
                <button
                  onClick={handleUpgrade}
                  className="text-xs px-3 py-1 bg-blue-500 hover:bg-blue-600 rounded-lg transition-colors text-white"
                >
                  Upgrade Now
                </button>
              </div>
            </div>
          </div>
        )}

        {/* Upgrading banner */}
        {upgrading && upgradeStatus?.in_progress && (
          <div className="bg-emerald-500/20 border-b border-emerald-500/30 px-4 py-2">
            <div className="flex items-center gap-2 text-emerald-200 max-w-7xl mx-auto">
              <Loader2 className="w-4 h-4 animate-spin" />
              <span className="text-sm">
                {upgradeStatus.message || 'Upgrading FastCP...'}
              </span>
            </div>
          </div>
        )}

        {/* Upgrade completed banner */}
        {upgradeStatus && !upgradeStatus.in_progress && upgradeStatus.success && (
          <div className="bg-emerald-500/20 border-b border-emerald-500/30 px-4 py-2">
            <div className="flex items-center gap-2 text-emerald-200 max-w-7xl mx-auto">
              <CheckCircle className="w-4 h-4" />
              <span className="text-sm">
                Upgrade completed successfully! Restarting...
              </span>
            </div>
          </div>
        )}

        {/* Upgrade failed banner */}
        {upgradeStatus && !upgradeStatus.in_progress && upgradeStatus.error && (
          <div className="bg-red-500/20 border-b border-red-500/30 px-4 py-2">
            <div className="flex items-center justify-between max-w-7xl mx-auto">
              <div className="flex items-center gap-2 text-red-200">
                <AlertCircle className="w-4 h-4" />
                <span className="text-sm">
                  Upgrade failed: {upgradeStatus.error}
                </span>
              </div>
              <button
                onClick={() => setUpgradeStatus(null)}
                className="text-xs px-3 py-1 bg-red-500/30 hover:bg-red-500/50 rounded-lg transition-colors text-red-100"
              >
                Dismiss
              </button>
            </div>
          </div>
        )}

        {/* Impersonation banner */}
        {isImpersonating && (
          <div className="bg-amber-500/20 border-b border-amber-500/30 px-4 py-2">
            <div className="flex items-center justify-between max-w-7xl mx-auto">
              <div className="flex items-center gap-2 text-amber-200">
                <UserX className="w-4 h-4" />
                <span className="text-sm">
                  Viewing as <strong>{user?.username}</strong>
                  <span className="text-amber-300/70 ml-2">(logged in as {realUser?.username})</span>
                </span>
              </div>
              <button
                onClick={handleStopImpersonating}
                className="text-xs px-3 py-1 bg-amber-500/30 hover:bg-amber-500/50 rounded-lg transition-colors text-amber-100"
              >
                Exit Impersonation
              </button>
            </div>
          </div>
        )}

        {/* Top bar */}
        <header className="sticky top-0 z-30 bg-background/80 backdrop-blur-xl border-b border-border">
          <div className="flex items-center gap-4 px-4 py-3 lg:px-6">
            <button
              className="lg:hidden p-2 -ml-2 text-muted-foreground hover:text-foreground"
              onClick={() => setSidebarOpen(true)}
            >
              <Menu className="w-5 h-5" />
            </button>
            <div className="flex-1" />
          </div>
        </header>

        {/* Page content */}
        <main className="p-4 lg:p-6">{children}</main>
      </div>

      {/* Upgrade Details Modal */}
      {showUpgradeModal && versionInfo && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
          <div className="bg-card border border-border rounded-xl w-full max-w-2xl max-h-[80vh] overflow-hidden m-4">
            <div className="flex items-center justify-between px-6 py-4 border-b border-border">
              <div>
                <h2 className="text-xl font-semibold">{versionInfo.release_name}</h2>
                <p className="text-sm text-muted-foreground">
                  Published {versionInfo.published_at ? new Date(versionInfo.published_at).toLocaleDateString() : 'recently'}
                </p>
              </div>
              <button
                onClick={() => setShowUpgradeModal(false)}
                className="p-2 hover:bg-secondary rounded-lg transition-colors"
              >
                <X className="w-5 h-5" />
              </button>
            </div>

            <div className="p-6 overflow-y-auto max-h-[50vh]">
              <div className="flex items-center gap-4 mb-4">
                <div className="flex-1">
                  <p className="text-sm text-muted-foreground">Current Version</p>
                  <p className="font-mono">v{versionInfo.current_version}</p>
                </div>
                <div className="text-2xl text-muted-foreground">→</div>
                <div className="flex-1">
                  <p className="text-sm text-muted-foreground">New Version</p>
                  <p className="font-mono text-emerald-400">v{versionInfo.latest_version}</p>
                </div>
              </div>

              {versionInfo.changelog && (
                <div className="mt-4">
                  <h3 className="font-medium mb-2">Changelog</h3>
                  <div className="bg-secondary/50 rounded-lg p-4 prose prose-sm prose-invert max-w-none">
                    <pre className="whitespace-pre-wrap text-sm text-muted-foreground">
                      {versionInfo.changelog}
                    </pre>
                  </div>
                </div>
              )}

              {versionInfo.release_url && (
                <a
                  href={versionInfo.release_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-block mt-4 text-sm text-blue-400 hover:text-blue-300"
                >
                  View on GitHub →
                </a>
              )}
            </div>

            <div className="flex gap-3 px-6 py-4 border-t border-border bg-secondary/30">
              <button
                onClick={() => setShowUpgradeModal(false)}
                className="flex-1 px-4 py-2 bg-secondary hover:bg-secondary/80 rounded-lg transition-colors"
              >
                Close
              </button>
              <button
                onClick={() => {
                  setShowUpgradeModal(false)
                  handleUpgrade()
                }}
                disabled={upgrading}
                className="flex-1 flex items-center justify-center gap-2 px-4 py-2 bg-emerald-500 hover:bg-emerald-600 text-white rounded-lg transition-colors disabled:opacity-50"
              >
                {upgrading ? (
                  <>
                    <Loader2 className="w-4 h-4 animate-spin" />
                    Upgrading...
                  </>
                ) : (
                  <>
                    <Download className="w-4 h-4" />
                    Upgrade Now
                  </>
                )}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

