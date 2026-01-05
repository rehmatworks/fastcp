import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  Globe,
  Server,
  Cpu,
  HardDrive,
  Clock,
  Plus,
  ArrowRight,
  Activity,
  Zap,
  TrendingUp,
} from 'lucide-react'
import { api } from '@/lib/api'
import { useAuth } from '@/hooks/useAuth'
import { formatBytes, formatUptime, getStatusBgColor } from '@/lib/utils'
import type { Stats, Site, PHPInstance } from '@/types'

export function DashboardPage() {
  const { user } = useAuth()
  const isAdmin = user?.role === 'admin'
  const [stats, setStats] = useState<Stats | null>(null)
  const [sites, setSites] = useState<Site[]>([])
  const [phpInstances, setPHPInstances] = useState<PHPInstance[]>([])
  const [isLoading, setIsLoading] = useState(true)

  useEffect(() => {
    async function fetchData() {
      try {
        // Only fetch PHP instances for admins
        const fetchPromises: Promise<any>[] = [
          api.getStats(),
          api.getSites(),
        ]
        if (isAdmin) {
          fetchPromises.push(api.getPHPInstances())
        }

        const results = await Promise.all(fetchPromises)
        setStats(results[0])
        setSites(results[1].sites || [])
        if (isAdmin && results[2]) {
          setPHPInstances(results[2].instances || [])
        }
      } catch (error) {
        console.error('Failed to fetch dashboard data:', error)
      } finally {
        setIsLoading(false)
      }
    }
    fetchData()
  }, [isAdmin])

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="w-10 h-10 border-2 border-emerald-500 border-t-transparent rounded-full animate-spin" />
      </div>
    )
  }

  // Different stats for admin vs regular users
  const statCards = isAdmin ? [
    {
      name: 'Total Sites',
      value: stats?.total_sites || 0,
      subtext: `${stats?.active_sites || 0} active`,
      icon: Globe,
      gradient: 'from-emerald-500 to-teal-600',
      bgGradient: 'from-emerald-500/10 to-teal-500/5',
      iconBg: 'bg-emerald-500/10',
      iconColor: 'text-emerald-400',
    },
    {
      name: 'PHP Instances',
      value: phpInstances.filter((p) => p.status === 'running').length,
      subtext: `of ${phpInstances.length} configured`,
      icon: Server,
      gradient: 'from-blue-500 to-indigo-600',
      bgGradient: 'from-blue-500/10 to-indigo-500/5',
      iconBg: 'bg-blue-500/10',
      iconColor: 'text-blue-400',
    },
    {
      name: 'Memory Usage',
      value: formatBytes(stats?.memory_usage || 0),
      subtext: `of ${formatBytes(stats?.memory_total || 0)}`,
      icon: Cpu,
      gradient: 'from-purple-500 to-pink-600',
      bgGradient: 'from-purple-500/10 to-pink-500/5',
      iconBg: 'bg-purple-500/10',
      iconColor: 'text-purple-400',
    },
    {
      name: 'Uptime',
      value: formatUptime(stats?.uptime || 0),
      subtext: 'System running',
      icon: Clock,
      gradient: 'from-amber-500 to-orange-600',
      bgGradient: 'from-amber-500/10 to-orange-500/5',
      iconBg: 'bg-amber-500/10',
      iconColor: 'text-amber-400',
    },
  ] : [
    {
      name: 'My Sites',
      value: sites.length,
      subtext: `${sites.filter(s => s.status === 'active').length} active`,
      icon: Globe,
      gradient: 'from-emerald-500 to-teal-600',
      bgGradient: 'from-emerald-500/10 to-teal-500/5',
      iconBg: 'bg-emerald-500/10',
      iconColor: 'text-emerald-400',
    },
    {
      name: 'Disk Usage',
      value: formatBytes(stats?.disk_usage || 0),
      subtext: 'Used by your sites',
      icon: HardDrive,
      gradient: 'from-blue-500 to-indigo-600',
      bgGradient: 'from-blue-500/10 to-indigo-500/5',
      iconBg: 'bg-blue-500/10',
      iconColor: 'text-blue-400',
    },
  ]

  return (
    <div className="space-y-8 animate-fade-in">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Dashboard</h1>
          <p className="text-muted-foreground mt-1">Welcome back, {user?.username}</p>
        </div>
        <Link
          to="/sites/new"
          className="flex items-center gap-2 px-5 py-2.5 bg-gradient-to-r from-emerald-500 to-teal-600 hover:from-emerald-600 hover:to-teal-700 text-white font-medium rounded-xl transition-all duration-200 shadow-lg shadow-emerald-500/20 btn-lift"
        >
          <Plus className="w-4 h-4" />
          New Site
        </Link>
      </div>

      {/* Stats Grid */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        {statCards.map((stat, index) => (
          <div
            key={stat.name}
            className="group relative bg-card border border-white/[0.06] rounded-2xl p-5 hover:border-white/[0.1] transition-all duration-300 card-shine overflow-hidden"
            style={{ animationDelay: `${index * 50}ms` }}
          >
            {/* Background gradient */}
            <div className={`absolute inset-0 bg-gradient-to-br ${stat.bgGradient} opacity-50`} />
            
            <div className="relative flex items-start justify-between">
              <div>
                <p className="text-sm text-muted-foreground font-medium">{stat.name}</p>
                <p className="text-3xl font-bold mt-2 tracking-tight">{stat.value}</p>
                <p className="text-xs text-muted-foreground mt-2 flex items-center gap-1">
                  <TrendingUp className="w-3 h-3" />
                  {stat.subtext}
                </p>
              </div>
              <div
                className={`w-12 h-12 rounded-xl bg-gradient-to-br ${stat.gradient} flex items-center justify-center shadow-lg`}
              >
                <stat.icon className="w-6 h-6 text-white" />
              </div>
            </div>
          </div>
        ))}
      </div>

      <div className={`grid grid-cols-1 ${isAdmin ? 'lg:grid-cols-2' : ''} gap-6`}>
        {/* Recent Sites */}
        <div className="bg-card border border-white/[0.06] rounded-2xl overflow-hidden">
          <div className="flex items-center justify-between px-6 py-4 border-b border-white/[0.06]">
            <h2 className="font-semibold">{isAdmin ? 'Recent Sites' : 'My Sites'}</h2>
            <Link
              to="/sites"
              className="text-sm text-emerald-400 hover:text-emerald-300 flex items-center gap-1 group"
            >
              View all
              <ArrowRight className="w-4 h-4 group-hover:translate-x-0.5 transition-transform" />
            </Link>
          </div>
          <div className="divide-y divide-white/[0.04]">
            {sites.length === 0 ? (
              <div className="px-6 py-12 text-center">
                <div className="w-16 h-16 rounded-2xl bg-emerald-500/10 flex items-center justify-center mx-auto mb-4">
                  <Globe className="w-8 h-8 text-emerald-400/50" />
                </div>
                <p className="text-muted-foreground">No sites yet</p>
                <Link
                  to="/sites/new"
                  className="text-sm text-emerald-400 hover:text-emerald-300 mt-2 inline-flex items-center gap-1"
                >
                  Create your first site
                  <ArrowRight className="w-4 h-4" />
                </Link>
              </div>
            ) : (
              sites.slice(0, 5).map((site) => (
                <Link
                  key={site.id}
                  to={`/sites/${site.id}`}
                  className="flex items-center gap-4 px-6 py-4 hover:bg-white/[0.02] transition-colors group"
                >
                  <div className="w-10 h-10 rounded-xl bg-gradient-to-br from-emerald-500/20 to-teal-500/10 flex items-center justify-center border border-emerald-500/20">
                    <Globe className="w-5 h-5 text-emerald-400" />
                  </div>
                  <div className="flex-1 min-w-0">
                    <p className="font-medium truncate group-hover:text-emerald-400 transition-colors">{site.name}</p>
                    <p className="text-sm text-muted-foreground truncate">
                      {site.domain}
                    </p>
                  </div>
                  <span
                    className={`text-xs px-2.5 py-1 rounded-full border font-medium ${getStatusBgColor(
                      site.status
                    )}`}
                  >
                    {site.status}
                  </span>
                </Link>
              ))
            )}
          </div>
        </div>

        {/* PHP Instances - Admin only */}
        {isAdmin && (
          <div className="bg-card border border-white/[0.06] rounded-2xl overflow-hidden">
            <div className="flex items-center justify-between px-6 py-4 border-b border-white/[0.06]">
              <h2 className="font-semibold">PHP Instances</h2>
              <Link
                to="/php"
                className="text-sm text-emerald-400 hover:text-emerald-300 flex items-center gap-1 group"
              >
                Manage
                <ArrowRight className="w-4 h-4 group-hover:translate-x-0.5 transition-transform" />
              </Link>
            </div>
            <div className="divide-y divide-white/[0.04]">
              {phpInstances.length === 0 ? (
                <div className="px-6 py-12 text-center">
                  <div className="w-16 h-16 rounded-2xl bg-blue-500/10 flex items-center justify-center mx-auto mb-4">
                    <Server className="w-8 h-8 text-blue-400/50" />
                  </div>
                  <p className="text-muted-foreground">No PHP instances configured</p>
                </div>
              ) : (
                phpInstances.map((instance) => (
                  <div
                    key={instance.version}
                    className="flex items-center gap-4 px-6 py-4"
                  >
                    <div
                      className={`w-10 h-10 rounded-xl flex items-center justify-center border ${
                        instance.status === 'running'
                          ? 'bg-emerald-500/10 border-emerald-500/20'
                          : 'bg-white/[0.02] border-white/[0.06]'
                      }`}
                    >
                      <Server
                        className={`w-5 h-5 ${
                          instance.status === 'running'
                            ? 'text-emerald-400'
                            : 'text-muted-foreground'
                        }`}
                      />
                    </div>
                    <div className="flex-1">
                      <p className="font-medium">PHP {instance.version}</p>
                      <p className="text-sm text-muted-foreground">
                        {instance.site_count} sites • Port {instance.port}
                      </p>
                    </div>
                    <div className="flex items-center gap-3">
                      {instance.status === 'running' && (
                        <Activity className="w-4 h-4 text-emerald-400 animate-pulse" />
                      )}
                      <span
                        className={`text-xs px-2.5 py-1 rounded-full border font-medium ${getStatusBgColor(
                          instance.status
                        )}`}
                      >
                        {instance.status}
                      </span>
                    </div>
                  </div>
                ))
              )}
            </div>
          </div>
        )}
      </div>

      {/* Quick Actions */}
      <div className="bg-card border border-white/[0.06] rounded-2xl p-6">
        <h2 className="font-semibold mb-5">Quick Actions</h2>
        <div className={`grid grid-cols-2 ${isAdmin ? 'sm:grid-cols-4' : 'sm:grid-cols-2'} gap-3`}>
          <Link
            to="/sites/new"
            className="group flex flex-col items-center gap-3 p-5 rounded-xl bg-white/[0.02] hover:bg-white/[0.04] border border-transparent hover:border-emerald-500/20 transition-all duration-200"
          >
            <div className="w-12 h-12 rounded-xl bg-gradient-to-br from-emerald-500/20 to-teal-500/10 flex items-center justify-center group-hover:scale-110 transition-transform">
              <Plus className="w-6 h-6 text-emerald-400" />
            </div>
            <span className="text-sm font-medium">Add Site</span>
          </Link>
          <Link
            to="/sites"
            className="group flex flex-col items-center gap-3 p-5 rounded-xl bg-white/[0.02] hover:bg-white/[0.04] border border-transparent hover:border-blue-500/20 transition-all duration-200"
          >
            <div className="w-12 h-12 rounded-xl bg-gradient-to-br from-blue-500/20 to-indigo-500/10 flex items-center justify-center group-hover:scale-110 transition-transform">
              <Globe className="w-6 h-6 text-blue-400" />
            </div>
            <span className="text-sm font-medium">My Sites</span>
          </Link>
          {isAdmin && (
            <>
              <Link
                to="/php"
                className="group flex flex-col items-center gap-3 p-5 rounded-xl bg-white/[0.02] hover:bg-white/[0.04] border border-transparent hover:border-purple-500/20 transition-all duration-200"
              >
                <div className="w-12 h-12 rounded-xl bg-gradient-to-br from-purple-500/20 to-pink-500/10 flex items-center justify-center group-hover:scale-110 transition-transform">
                  <Server className="w-6 h-6 text-purple-400" />
                </div>
                <span className="text-sm font-medium">PHP Manager</span>
              </Link>
              <button
                onClick={() => api.reloadAll()}
                className="group flex flex-col items-center gap-3 p-5 rounded-xl bg-white/[0.02] hover:bg-white/[0.04] border border-transparent hover:border-amber-500/20 transition-all duration-200"
              >
                <div className="w-12 h-12 rounded-xl bg-gradient-to-br from-amber-500/20 to-orange-500/10 flex items-center justify-center group-hover:scale-110 transition-transform">
                  <Zap className="w-6 h-6 text-amber-400" />
                </div>
                <span className="text-sm font-medium">Reload All</span>
              </button>
            </>
          )}
        </div>
      </div>
    </div>
  )
}
