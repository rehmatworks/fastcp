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
  Sparkles,
  ArrowUpRight,
  Zap,
  Sun,
  Moon,
  Monitor,
} from 'lucide-react'
import { useState, useEffect, useCallback, useRef } from 'react'
import { cn } from '@/lib/utils'
import { useAuth } from '@/hooks/useAuth'
import { useTheme } from '@/hooks/useTheme'
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
  const { theme, resolvedTheme, setTheme } = useTheme()
  const [sidebarOpen, setSidebarOpen] = useState(false)
  const [versionInfo, setVersionInfo] = useState<VersionCheckResult | null>(null)
  const [upgradeStatus, setUpgradeStatus] = useState<UpgradeStatus | null>(null)
  const [upgrading, setUpgrading] = useState(false)
  const [showUpgradeModal, setShowUpgradeModal] = useState(false)
  const [showThemeMenu, setShowThemeMenu] = useState(false)
  const pollIntervalRef = useRef<NodeJS.Timeout | null>(null)

  const effectiveRole = isImpersonating ? realUser?.role : user?.role

  useEffect(() => {
    if (effectiveRole === 'admin') {
      checkForUpdates()
    }
  }, [effectiveRole])

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
        if (pollIntervalRef.current) {
          clearInterval(pollIntervalRef.current)
          pollIntervalRef.current = null
        }
        setUpgrading(false)

        if (status.success) {
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
    setShowUpgradeModal(false)

    try {
      await api.startUpgrade()
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
          'fixed inset-y-0 left-0 z-50 w-64 bg-card border-r border-border transform transition-transform duration-200 lg:translate-x-0',
          sidebarOpen ? 'translate-x-0' : '-translate-x-full'
        )}
      >
        <div className="flex flex-col h-full">
          {/* Logo */}
          <div className="flex items-center gap-3 px-5 py-5 border-b border-border">
            <div className="relative">
              <div className="w-9 h-9 rounded-lg bg-primary flex items-center justify-center">
                <Zap className="w-5 h-5 text-primary-foreground" />
              </div>
              {versionInfo?.update_available && (
                <span className="absolute -top-1 -right-1 w-2.5 h-2.5 rounded-full bg-blue-500 border-2 border-card" />
              )}
            </div>
            <div>
              <h1 className="font-semibold">FastCP</h1>
              <p className="text-xs text-muted-foreground font-mono">
                {versionInfo ? `v${versionInfo.current_version}` : '...'}
              </p>
            </div>
            <button
              className="ml-auto lg:hidden p-2 hover:bg-secondary rounded-lg transition-colors"
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
                      'flex items-center gap-3 px-3 py-2.5 rounded-xl text-sm font-medium transition-colors',
                      isActive
                        ? 'bg-primary/10 text-primary'
                        : 'text-muted-foreground hover:text-foreground hover:bg-secondary'
                    )}
                  >
                    <item.icon className="w-5 h-5" />
                    {item.name}
                  </Link>
                )
              })}
          </nav>

          {/* Update available card */}
          {versionInfo?.update_available && effectiveRole === 'admin' && !upgrading && (
            <div className="mx-3 mb-3">
              <button
                onClick={() => setShowUpgradeModal(true)}
                className="w-full flex items-center gap-3 p-3 bg-blue-500/10 hover:bg-blue-500/15 border border-blue-500/20 rounded-xl text-left transition-colors"
              >
                <div className="w-8 h-8 rounded-lg bg-blue-500/20 flex items-center justify-center">
                  <Sparkles className="w-4 h-4 text-blue-600 dark:text-blue-400" />
                </div>
                <div className="flex-1 min-w-0">
                  <p className="text-sm font-medium">Update Available</p>
                  <p className="text-xs text-muted-foreground truncate">
                    v{versionInfo.latest_version}
                  </p>
                </div>
              </button>
            </div>
          )}

          {/* Theme toggle */}
          <div className="px-3 pb-3">
            <div className="relative">
              <button
                onClick={() => setShowThemeMenu(!showThemeMenu)}
                className="w-full flex items-center gap-3 px-3 py-2.5 rounded-xl text-sm font-medium text-muted-foreground hover:text-foreground hover:bg-secondary transition-colors"
              >
                {resolvedTheme === 'dark' ? (
                  <Moon className="w-5 h-5" />
                ) : (
                  <Sun className="w-5 h-5" />
                )}
                <span className="capitalize">{theme === 'system' ? `System` : theme}</span>
              </button>
              
              {showThemeMenu && (
                <>
                  <div className="fixed inset-0 z-10" onClick={() => setShowThemeMenu(false)} />
                  <div className="absolute bottom-full left-0 right-0 mb-2 bg-card border border-border rounded-xl shadow-lg z-20 overflow-hidden">
                    <div className="py-1">
                      {[
                        { value: 'light', icon: Sun, label: 'Light' },
                        { value: 'dark', icon: Moon, label: 'Dark' },
                        { value: 'system', icon: Monitor, label: 'System' },
                      ].map((option) => (
                        <button
                          key={option.value}
                          onClick={() => { setTheme(option.value as any); setShowThemeMenu(false) }}
                          className={cn(
                            "flex items-center gap-3 w-full px-3 py-2.5 text-sm text-left transition-colors",
                            theme === option.value ? "bg-primary/10 text-primary" : "hover:bg-secondary"
                          )}
                        >
                          <option.icon className="w-4 h-4" />
                          {option.label}
                        </button>
                      ))}
                    </div>
                  </div>
                </>
              )}
            </div>
          </div>

          {/* User section */}
          <div className="p-3 border-t border-border">
            <div className="flex items-center gap-3 px-2 py-2">
              <div className="w-9 h-9 rounded-lg bg-secondary flex items-center justify-center">
                <span className="text-sm font-medium">
                  {user?.username?.charAt(0).toUpperCase() || 'A'}
                </span>
              </div>
              <div className="flex-1 min-w-0">
                <p className="text-sm font-medium truncate">{user?.username}</p>
                <p className="text-xs text-muted-foreground capitalize">{user?.role}</p>
              </div>
              <button
                onClick={handleLogout}
                className="p-2 text-muted-foreground hover:text-red-500 hover:bg-red-500/10 rounded-lg transition-colors"
                title="Sign out"
              >
                <LogOut className="w-4 h-4" />
              </button>
            </div>
          </div>
        </div>
      </aside>

      {/* Main content */}
      <div className="lg:pl-64">
        {/* Status banners */}
        {upgrading && upgradeStatus?.in_progress && (
          <div className="bg-blue-500/10 border-b border-blue-500/20 px-6 py-3">
            <div className="flex items-center gap-3 max-w-7xl mx-auto">
              <Loader2 className="w-4 h-4 text-blue-500 animate-spin" />
              <p className="text-sm font-medium text-blue-700 dark:text-blue-300">
                {upgradeStatus.message || 'Upgrading FastCP...'}
              </p>
            </div>
          </div>
        )}

        {upgradeStatus && !upgradeStatus.in_progress && upgradeStatus.success && (
          <div className="bg-emerald-500/10 border-b border-emerald-500/20 px-6 py-3">
            <div className="flex items-center gap-3 max-w-7xl mx-auto">
              <CheckCircle className="w-4 h-4 text-emerald-500" />
              <p className="text-sm font-medium text-emerald-700 dark:text-emerald-300">
                Upgrade completed! Restarting...
              </p>
            </div>
          </div>
        )}

        {upgradeStatus && !upgradeStatus.in_progress && upgradeStatus.error && (
          <div className="bg-red-500/10 border-b border-red-500/20 px-6 py-3">
            <div className="flex items-center justify-between max-w-7xl mx-auto">
              <div className="flex items-center gap-3">
                <AlertCircle className="w-4 h-4 text-red-500" />
                <p className="text-sm font-medium text-red-700 dark:text-red-300">
                  Upgrade failed: {upgradeStatus.error}
                </p>
              </div>
              <button
                onClick={() => setUpgradeStatus(null)}
                className="text-xs px-3 py-1.5 bg-red-500/20 hover:bg-red-500/30 rounded-lg transition-colors text-red-600 dark:text-red-300"
              >
                Dismiss
              </button>
            </div>
          </div>
        )}

        {isImpersonating && (
          <div className="bg-amber-500/10 border-b border-amber-500/20 px-6 py-3">
            <div className="flex items-center justify-between max-w-7xl mx-auto">
              <div className="flex items-center gap-3">
                <UserX className="w-4 h-4 text-amber-500" />
                <p className="text-sm font-medium text-amber-700 dark:text-amber-300">
                  Viewing as <span className="font-semibold">{user?.username}</span>
                </p>
              </div>
              <button
                onClick={handleStopImpersonating}
                className="text-xs px-3 py-1.5 bg-amber-500/20 hover:bg-amber-500/30 rounded-lg transition-colors text-amber-600 dark:text-amber-300 font-medium"
              >
                Exit
              </button>
            </div>
          </div>
        )}

        {/* Mobile header */}
        <header className="sticky top-0 z-30 bg-background/95 backdrop-blur border-b border-border lg:hidden">
          <div className="flex items-center gap-4 px-4 py-3">
            <button
              className="p-2 -ml-2 text-muted-foreground hover:text-foreground hover:bg-secondary rounded-lg transition-colors"
              onClick={() => setSidebarOpen(true)}
            >
              <Menu className="w-5 h-5" />
            </button>
            <div className="flex items-center gap-2">
              <div className="w-7 h-7 rounded-lg bg-primary flex items-center justify-center">
                <Zap className="w-4 h-4 text-primary-foreground" />
              </div>
              <span className="font-semibold">FastCP</span>
            </div>
          </div>
        </header>

        {/* Page content */}
        <main className="p-4 lg:p-8">{children}</main>
      </div>

      {/* Upgrade Modal */}
      {showUpgradeModal && versionInfo && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
          <div className="bg-card border border-border rounded-2xl w-full max-w-lg shadow-xl">
            {/* Header */}
            <div className="flex items-center justify-between px-6 py-4 border-b border-border">
              <div className="flex items-center gap-3">
                <div className="w-10 h-10 rounded-xl bg-blue-500/10 flex items-center justify-center">
                  <Sparkles className="w-5 h-5 text-blue-600 dark:text-blue-400" />
                </div>
                <div>
                  <h2 className="font-semibold">{versionInfo.release_name}</h2>
                  <p className="text-xs text-muted-foreground">
                    {versionInfo.published_at && new Date(versionInfo.published_at).toLocaleDateString()}
                  </p>
                </div>
              </div>
              <button
                onClick={() => setShowUpgradeModal(false)}
                className="p-2 hover:bg-secondary rounded-lg transition-colors"
              >
                <X className="w-5 h-5" />
              </button>
            </div>

            {/* Content */}
            <div className="p-6">
              {/* Version comparison */}
              <div className="flex items-center gap-4 p-4 bg-secondary rounded-xl mb-4">
                <div className="flex-1 text-center">
                  <p className="text-xs text-muted-foreground mb-1">Current</p>
                  <p className="font-mono font-medium">v{versionInfo.current_version}</p>
                </div>
                <ArrowUpRight className="w-5 h-5 text-primary" />
                <div className="flex-1 text-center">
                  <p className="text-xs text-muted-foreground mb-1">New</p>
                  <p className="font-mono font-medium text-primary">v{versionInfo.latest_version}</p>
                </div>
              </div>

              {/* Changelog */}
              {versionInfo.changelog && (
                <div className="max-h-48 overflow-y-auto">
                  <p className="text-xs text-muted-foreground uppercase tracking-wide mb-2">What's New</p>
                  <div className="bg-secondary rounded-xl p-4">
                    <pre className="whitespace-pre-wrap text-sm font-mono leading-relaxed text-foreground">
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
                  className="inline-flex items-center gap-1 mt-4 text-sm text-primary hover:underline"
                >
                  View on GitHub
                  <ArrowUpRight className="w-3 h-3" />
                </a>
              )}
            </div>

            {/* Footer */}
            <div className="flex gap-3 px-6 pb-6">
              <button
                onClick={() => setShowUpgradeModal(false)}
                className="flex-1 px-4 py-2.5 bg-secondary hover:bg-secondary/80 rounded-xl font-medium transition-colors"
              >
                Later
              </button>
              <button
                onClick={handleUpgrade}
                disabled={upgrading}
                className="flex-1 flex items-center justify-center gap-2 px-4 py-2.5 bg-primary hover:bg-primary/90 text-primary-foreground font-medium rounded-xl transition-colors disabled:opacity-50"
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
