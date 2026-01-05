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
        <div className="w-8 h-8 border-2 border-emerald-500 border-t-transparent rounded-full animate-spin" />
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
      color: 'from-emerald-500 to-emerald-600',
    },
    {
      name: 'PHP Instances',
      value: phpInstances.filter((p) => p.status === 'running').length,
      subtext: `of ${phpInstances.length} configured`,
      icon: Server,
      color: 'from-blue-500 to-blue-600',
    },
    {
      name: 'Memory Usage',
      value: formatBytes(stats?.memory_usage || 0),
      subtext: `of ${formatBytes(stats?.memory_total || 0)}`,
      icon: Cpu,
      color: 'from-purple-500 to-purple-600',
    },
    {
      name: 'Uptime',
      value: formatUptime(stats?.uptime || 0),
      subtext: 'System running',
      icon: Clock,
      color: 'from-amber-500 to-amber-600',
    },
  ] : [
    {
      name: 'My Sites',
      value: sites.length,
      subtext: `${sites.filter(s => s.status === 'active').length} active`,
      icon: Globe,
      color: 'from-emerald-500 to-emerald-600',
    },
    {
      name: 'Disk Usage',
      value: formatBytes(stats?.disk_usage || 0),
      subtext: 'Used by your sites',
      icon: HardDrive,
      color: 'from-blue-500 to-blue-600',
    },
  ]

  return (
    <div className="space-y-6 animate-fade-in">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">Dashboard</h1>
          <p className="text-muted-foreground">Welcome back to FastCP</p>
        </div>
        <Link
          to="/sites/new"
          className="flex items-center gap-2 px-4 py-2 bg-gradient-to-r from-emerald-500 to-emerald-600 hover:from-emerald-600 hover:to-emerald-700 text-white font-medium rounded-lg transition-all duration-200"
        >
          <Plus className="w-4 h-4" />
          New Site
        </Link>
      </div>

      {/* Stats Grid */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        {statCards.map((stat) => (
          <div
            key={stat.name}
            className="bg-card border border-border rounded-xl p-5 hover:border-emerald-500/30 transition-colors"
          >
            <div className="flex items-start justify-between">
              <div>
                <p className="text-sm text-muted-foreground">{stat.name}</p>
                <p className="text-2xl font-bold mt-1">{stat.value}</p>
                <p className="text-xs text-muted-foreground mt-1">{stat.subtext}</p>
              </div>
              <div
                className={`w-10 h-10 rounded-lg bg-gradient-to-br ${stat.color} flex items-center justify-center`}
              >
                <stat.icon className="w-5 h-5 text-white" />
              </div>
            </div>
          </div>
        ))}
      </div>

      <div className={`grid grid-cols-1 ${isAdmin ? 'lg:grid-cols-2' : ''} gap-6`}>
        {/* Recent Sites */}
        <div className="bg-card border border-border rounded-xl">
          <div className="flex items-center justify-between px-5 py-4 border-b border-border">
            <h2 className="font-semibold">{isAdmin ? 'Recent Sites' : 'My Sites'}</h2>
            <Link
              to="/sites"
              className="text-sm text-emerald-400 hover:text-emerald-300 flex items-center gap-1"
            >
              View all
              <ArrowRight className="w-4 h-4" />
            </Link>
          </div>
          <div className="divide-y divide-border">
            {sites.length === 0 ? (
              <div className="px-5 py-8 text-center text-muted-foreground">
                <Globe className="w-10 h-10 mx-auto mb-3 opacity-50" />
                <p>No sites yet</p>
                <Link
                  to="/sites/new"
                  className="text-sm text-emerald-400 hover:text-emerald-300 mt-2 inline-block"
                >
                  Create your first site
                </Link>
              </div>
            ) : (
              sites.slice(0, 5).map((site) => (
                <Link
                  key={site.id}
                  to={`/sites/${site.id}`}
                  className="flex items-center gap-4 px-5 py-3 hover:bg-secondary/50 transition-colors"
                >
                  <div className="w-9 h-9 rounded-lg bg-gradient-to-br from-emerald-500/20 to-emerald-600/20 flex items-center justify-center border border-emerald-500/20">
                    <Globe className="w-4 h-4 text-emerald-400" />
                  </div>
                  <div className="flex-1 min-w-0">
                    <p className="font-medium truncate">{site.name}</p>
                    <p className="text-sm text-muted-foreground truncate">
                      {site.domain}
                    </p>
                  </div>
                  <span
                    className={`text-xs px-2 py-1 rounded-full border ${getStatusBgColor(
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
          <div className="bg-card border border-border rounded-xl">
            <div className="flex items-center justify-between px-5 py-4 border-b border-border">
              <h2 className="font-semibold">PHP Instances</h2>
              <Link
                to="/php"
                className="text-sm text-emerald-400 hover:text-emerald-300 flex items-center gap-1"
              >
                Manage
                <ArrowRight className="w-4 h-4" />
              </Link>
            </div>
            <div className="divide-y divide-border">
              {phpInstances.length === 0 ? (
                <div className="px-5 py-8 text-center text-muted-foreground">
                  <Server className="w-10 h-10 mx-auto mb-3 opacity-50" />
                  <p>No PHP instances configured</p>
                </div>
              ) : (
                phpInstances.map((instance) => (
                  <div
                    key={instance.version}
                    className="flex items-center gap-4 px-5 py-3"
                  >
                    <div
                      className={`w-9 h-9 rounded-lg flex items-center justify-center border ${
                        instance.status === 'running'
                          ? 'bg-emerald-500/10 border-emerald-500/20'
                          : 'bg-secondary border-border'
                      }`}
                    >
                      <Server
                        className={`w-4 h-4 ${
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
                    <div className="flex items-center gap-2">
                      {instance.status === 'running' && (
                        <Activity className="w-4 h-4 text-emerald-400 animate-pulse" />
                      )}
                      <span
                        className={`text-xs px-2 py-1 rounded-full border ${getStatusBgColor(
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
      <div className="bg-card border border-border rounded-xl p-5">
        <h2 className="font-semibold mb-4">Quick Actions</h2>
        <div className={`grid grid-cols-2 ${isAdmin ? 'sm:grid-cols-4' : 'sm:grid-cols-2'} gap-3`}>
          <Link
            to="/sites/new"
            className="flex flex-col items-center gap-2 p-4 rounded-lg bg-secondary hover:bg-secondary/80 transition-colors"
          >
            <Plus className="w-5 h-5 text-emerald-400" />
            <span className="text-sm">Add Site</span>
          </Link>
          <Link
            to="/sites"
            className="flex flex-col items-center gap-2 p-4 rounded-lg bg-secondary hover:bg-secondary/80 transition-colors"
          >
            <Globe className="w-5 h-5 text-blue-400" />
            <span className="text-sm">My Sites</span>
          </Link>
          {isAdmin && (
            <>
              <Link
                to="/php"
                className="flex flex-col items-center gap-2 p-4 rounded-lg bg-secondary hover:bg-secondary/80 transition-colors"
              >
                <Server className="w-5 h-5 text-purple-400" />
                <span className="text-sm">PHP Manager</span>
              </Link>
              <button
                onClick={() => api.reloadAll()}
                className="flex flex-col items-center gap-2 p-4 rounded-lg bg-secondary hover:bg-secondary/80 transition-colors"
              >
                <Activity className="w-5 h-5 text-amber-400" />
                <span className="text-sm">Reload All</span>
              </button>
            </>
          )}
        </div>
      </div>
    </div>
  )
}

