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
    setShowUpgradeModal(false)

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
          className="fixed inset-0 bg-black/60 backdrop-blur-sm z-40 lg:hidden"
          onClick={() => setSidebarOpen(false)}
        />
      )}

      {/* Sidebar */}
      <aside
        className={cn(
          'fixed inset-y-0 left-0 z-50 w-72 bg-card/80 backdrop-blur-xl border-r border-white/[0.06] transform transition-transform duration-300 ease-out lg:translate-x-0',
          sidebarOpen ? 'translate-x-0' : '-translate-x-full'
        )}
      >
        <div className="flex flex-col h-full">
          {/* Logo */}
          <div className="flex items-center gap-3 px-6 py-6">
            <div className="relative">
              <div className="w-10 h-10 rounded-xl bg-gradient-to-br from-emerald-400 via-emerald-500 to-teal-600 flex items-center justify-center shadow-lg shadow-emerald-500/20">
                <Zap className="w-5 h-5 text-white" />
              </div>
              {versionInfo?.update_available && (
                <span className="absolute -top-1 -right-1 w-3 h-3 rounded-full bg-blue-500 border-2 border-card status-dot-active" />
              )}
            </div>
            <div>
              <h1 className="font-semibold text-lg tracking-tight">FastCP</h1>
              <p className="text-xs text-muted-foreground font-mono">
                {versionInfo ? `v${versionInfo.current_version}` : 'Loading...'}
              </p>
            </div>
            <button
              className="ml-auto lg:hidden p-2 hover:bg-white/5 rounded-lg transition-colors"
              onClick={() => setSidebarOpen(false)}
            >
              <X className="w-5 h-5" />
            </button>
          </div>

          {/* Navigation */}
          <nav className="flex-1 px-4 py-2 space-y-1">
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
                      'group flex items-center gap-3 px-4 py-3 rounded-xl text-sm font-medium transition-all duration-200',
                      isActive
                        ? 'bg-gradient-to-r from-emerald-500/15 to-teal-500/10 text-emerald-400 shadow-sm'
                        : 'text-muted-foreground hover:text-foreground hover:bg-white/[0.04]'
                    )}
                  >
                    <item.icon className={cn(
                      "w-5 h-5 transition-transform duration-200",
                      isActive ? "" : "group-hover:scale-110"
                    )} />
                    {item.name}
                    {isActive && (
                      <div className="ml-auto w-1.5 h-1.5 rounded-full bg-emerald-400" />
                    )}
                  </Link>
                )
              })}
          </nav>

          {/* Update available card in sidebar */}
          {versionInfo?.update_available && effectiveRole === 'admin' && !upgrading && (
            <div className="mx-4 mb-4">
              <button
                onClick={() => setShowUpgradeModal(true)}
                className="w-full group relative overflow-hidden rounded-xl p-4 text-left transition-all duration-300 hover:scale-[1.02]"
              >
                {/* Animated gradient background */}
                <div className="absolute inset-0 bg-gradient-to-br from-blue-600/20 via-indigo-600/20 to-purple-600/20 rounded-xl" />
                <div className="absolute inset-0 bg-gradient-to-br from-blue-500/10 via-transparent to-purple-500/10 opacity-0 group-hover:opacity-100 transition-opacity duration-300" />
                <div className="absolute inset-[1px] bg-card/90 rounded-[11px]" />
                
                <div className="relative flex items-start gap-3">
                  <div className="w-9 h-9 rounded-lg bg-gradient-to-br from-blue-500 to-indigo-600 flex items-center justify-center flex-shrink-0 shadow-lg shadow-blue-500/20">
                    <Sparkles className="w-4 h-4 text-white" />
                  </div>
                  <div className="flex-1 min-w-0">
                    <p className="text-sm font-medium text-foreground">Update Available</p>
                    <p className="text-xs text-muted-foreground mt-0.5 truncate">
                      {versionInfo.release_name}
                    </p>
                  </div>
                  <ArrowUpRight className="w-4 h-4 text-muted-foreground group-hover:text-blue-400 transition-colors flex-shrink-0 mt-1" />
                </div>
              </button>
            </div>
          )}

          {/* User section */}
          <div className="p-4 border-t border-white/[0.06]">
            <div className="flex items-center gap-3 px-2 py-2">
              <div className="w-10 h-10 rounded-xl bg-gradient-to-br from-slate-600 to-slate-700 flex items-center justify-center">
                <span className="text-white text-sm font-semibold">
                  {user?.username?.charAt(0).toUpperCase() || 'A'}
                </span>
              </div>
              <div className="flex-1 min-w-0">
                <p className="text-sm font-medium truncate">{user?.username}</p>
                <p className="text-xs text-muted-foreground capitalize">{user?.role}</p>
              </div>
              <button
                onClick={handleLogout}
                className="p-2.5 text-muted-foreground hover:text-red-400 hover:bg-red-500/10 rounded-lg transition-all duration-200"
                title="Sign out"
              >
                <LogOut className="w-4 h-4" />
              </button>
            </div>
          </div>
        </div>
      </aside>

      {/* Main content */}
      <div className="lg:pl-72">
        {/* Upgrading banner */}
        {upgrading && upgradeStatus?.in_progress && (
          <div className="bg-gradient-to-r from-emerald-500/10 via-teal-500/10 to-cyan-500/10 border-b border-emerald-500/20 px-6 py-3">
            <div className="flex items-center gap-3 max-w-7xl mx-auto">
              <div className="w-8 h-8 rounded-lg bg-emerald-500/20 flex items-center justify-center">
                <Loader2 className="w-4 h-4 text-emerald-400 animate-spin" />
              </div>
              <div className="flex-1">
                <p className="text-sm font-medium text-emerald-300">
                  {upgradeStatus.message || 'Upgrading FastCP...'}
                </p>
                <p className="text-xs text-emerald-400/60">Please wait, this may take a moment</p>
              </div>
            </div>
          </div>
        )}

        {/* Upgrade completed banner */}
        {upgradeStatus && !upgradeStatus.in_progress && upgradeStatus.success && (
          <div className="bg-gradient-to-r from-emerald-500/10 via-teal-500/10 to-cyan-500/10 border-b border-emerald-500/20 px-6 py-3">
            <div className="flex items-center gap-3 max-w-7xl mx-auto">
              <div className="w-8 h-8 rounded-lg bg-emerald-500/20 flex items-center justify-center">
                <CheckCircle className="w-4 h-4 text-emerald-400" />
              </div>
              <p className="text-sm font-medium text-emerald-300">
                Upgrade completed successfully! Restarting...
              </p>
            </div>
          </div>
        )}

        {/* Upgrade failed banner */}
        {upgradeStatus && !upgradeStatus.in_progress && upgradeStatus.error && (
          <div className="bg-gradient-to-r from-red-500/10 via-rose-500/10 to-pink-500/10 border-b border-red-500/20 px-6 py-3">
            <div className="flex items-center justify-between max-w-7xl mx-auto">
              <div className="flex items-center gap-3">
                <div className="w-8 h-8 rounded-lg bg-red-500/20 flex items-center justify-center">
                  <AlertCircle className="w-4 h-4 text-red-400" />
                </div>
                <p className="text-sm font-medium text-red-300">
                  Upgrade failed: {upgradeStatus.error}
                </p>
              </div>
              <button
                onClick={() => setUpgradeStatus(null)}
                className="text-xs px-3 py-1.5 bg-red-500/20 hover:bg-red-500/30 rounded-lg transition-colors text-red-300"
              >
                Dismiss
              </button>
            </div>
          </div>
        )}

        {/* Impersonation banner */}
        {isImpersonating && (
          <div className="bg-gradient-to-r from-amber-500/10 via-orange-500/10 to-yellow-500/10 border-b border-amber-500/20 px-6 py-3">
            <div className="flex items-center justify-between max-w-7xl mx-auto">
              <div className="flex items-center gap-3">
                <div className="w-8 h-8 rounded-lg bg-amber-500/20 flex items-center justify-center">
                  <UserX className="w-4 h-4 text-amber-400" />
                </div>
                <div>
                  <p className="text-sm font-medium text-amber-300">
                    Viewing as <span className="font-semibold">{user?.username}</span>
                  </p>
                  <p className="text-xs text-amber-400/60">Logged in as {realUser?.username}</p>
                </div>
              </div>
              <button
                onClick={handleStopImpersonating}
                className="text-xs px-4 py-1.5 bg-amber-500/20 hover:bg-amber-500/30 rounded-lg transition-colors text-amber-300 font-medium"
              >
                Exit Impersonation
              </button>
            </div>
          </div>
        )}

        {/* Mobile header */}
        <header className="sticky top-0 z-30 bg-background/80 backdrop-blur-xl border-b border-white/[0.06] lg:hidden">
          <div className="flex items-center gap-4 px-4 py-3">
            <button
              className="p-2 -ml-2 text-muted-foreground hover:text-foreground hover:bg-white/5 rounded-lg transition-colors"
              onClick={() => setSidebarOpen(true)}
            >
              <Menu className="w-5 h-5" />
            </button>
            <div className="flex items-center gap-2">
              <div className="w-7 h-7 rounded-lg bg-gradient-to-br from-emerald-400 to-teal-600 flex items-center justify-center">
                <Zap className="w-3.5 h-3.5 text-white" />
              </div>
              <span className="font-semibold">FastCP</span>
            </div>
          </div>
        </header>

        {/* Page content */}
        <main className="p-4 lg:p-8">{children}</main>
      </div>

      {/* Upgrade Details Modal */}
      {showUpgradeModal && versionInfo && (
        <div className="fixed inset-0 bg-black/60 backdrop-blur-sm flex items-center justify-center z-50 p-4">
          <div 
            className="bg-card border border-white/[0.08] rounded-2xl w-full max-w-2xl max-h-[85vh] overflow-hidden shadow-2xl shadow-black/50 animate-fade-in"
            onClick={(e) => e.stopPropagation()}
          >
            {/* Modal Header */}
            <div className="relative px-6 py-5 border-b border-white/[0.06]">
              <div className="absolute inset-0 bg-gradient-to-r from-blue-500/5 via-indigo-500/5 to-purple-500/5" />
              <div className="relative flex items-start justify-between">
                <div className="flex items-center gap-4">
                  <div className="w-12 h-12 rounded-xl bg-gradient-to-br from-blue-500 to-indigo-600 flex items-center justify-center shadow-lg shadow-blue-500/20">
                    <Sparkles className="w-6 h-6 text-white" />
                  </div>
                  <div>
                    <h2 className="text-xl font-semibold">{versionInfo.release_name}</h2>
                    <p className="text-sm text-muted-foreground mt-0.5">
                      Published {versionInfo.published_at ? new Date(versionInfo.published_at).toLocaleDateString('en-US', { month: 'long', day: 'numeric', year: 'numeric' }) : 'recently'}
                    </p>
                  </div>
                </div>
                <button
                  onClick={() => setShowUpgradeModal(false)}
                  className="p-2 hover:bg-white/5 rounded-lg transition-colors"
                >
                  <X className="w-5 h-5" />
                </button>
              </div>
            </div>

            {/* Modal Content */}
            <div className="p-6 overflow-y-auto max-h-[50vh]">
              {/* Version comparison */}
              <div className="flex items-center gap-4 p-4 bg-white/[0.02] rounded-xl border border-white/[0.06]">
                <div className="flex-1 text-center">
                  <p className="text-xs text-muted-foreground uppercase tracking-wider mb-1">Current</p>
                  <p className="text-lg font-mono font-medium">v{versionInfo.current_version}</p>
                </div>
                <div className="flex items-center justify-center w-10 h-10 rounded-full bg-gradient-to-br from-emerald-500/20 to-teal-500/20 border border-emerald-500/20">
                  <ArrowUpRight className="w-5 h-5 text-emerald-400" />
                </div>
                <div className="flex-1 text-center">
                  <p className="text-xs text-muted-foreground uppercase tracking-wider mb-1">New Version</p>
                  <p className="text-lg font-mono font-medium text-emerald-400">v{versionInfo.latest_version}</p>
                </div>
              </div>

              {/* Changelog */}
              {versionInfo.changelog && (
                <div className="mt-6">
                  <h3 className="text-sm font-medium text-muted-foreground uppercase tracking-wider mb-3">What's New</h3>
                  <div className="bg-white/[0.02] rounded-xl border border-white/[0.06] p-4">
                    <pre className="whitespace-pre-wrap text-sm text-foreground/80 font-mono leading-relaxed">
                      {versionInfo.changelog}
                    </pre>
                  </div>
                </div>
              )}

              {/* GitHub link */}
              {versionInfo.release_url && (
                <a
                  href={versionInfo.release_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex items-center gap-2 mt-4 text-sm text-blue-400 hover:text-blue-300 transition-colors"
                >
                  View full release on GitHub
                  <ArrowUpRight className="w-4 h-4" />
                </a>
              )}
            </div>

            {/* Modal Footer */}
            <div className="flex gap-3 px-6 py-4 border-t border-white/[0.06] bg-white/[0.02]">
              <button
                onClick={() => setShowUpgradeModal(false)}
                className="flex-1 px-4 py-2.5 bg-white/[0.05] hover:bg-white/[0.08] rounded-xl font-medium transition-colors"
              >
                Maybe Later
              </button>
              <button
                onClick={handleUpgrade}
                disabled={upgrading}
                className="flex-1 flex items-center justify-center gap-2 px-4 py-2.5 bg-gradient-to-r from-emerald-500 to-teal-600 hover:from-emerald-600 hover:to-teal-700 text-white font-medium rounded-xl transition-all duration-200 disabled:opacity-50 shadow-lg shadow-emerald-500/20 btn-lift"
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
