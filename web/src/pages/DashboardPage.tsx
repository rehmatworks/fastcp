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
        <div className="w-10 h-10 border-2 border-primary border-t-transparent rounded-full animate-spin" />
      </div>
    )
  }

  const statCards = isAdmin ? [
    {
      name: 'Total Sites',
      value: stats?.total_sites || 0,
      subtext: `${stats?.active_sites || 0} active`,
      icon: Globe,
      iconBg: 'bg-emerald-500/10 dark:bg-emerald-500/20',
      iconColor: 'text-emerald-600 dark:text-emerald-400',
    },
    {
      name: 'PHP Instances',
      value: phpInstances.filter((p) => p.status === 'running').length,
      subtext: `of ${phpInstances.length} configured`,
      icon: Server,
      iconBg: 'bg-blue-500/10 dark:bg-blue-500/20',
      iconColor: 'text-blue-600 dark:text-blue-400',
    },
    {
      name: 'Memory Usage',
      value: formatBytes(stats?.memory_usage || 0),
      subtext: `of ${formatBytes(stats?.memory_total || 0)}`,
      icon: Cpu,
      iconBg: 'bg-purple-500/10 dark:bg-purple-500/20',
      iconColor: 'text-purple-600 dark:text-purple-400',
    },
    {
      name: 'Uptime',
      value: formatUptime(stats?.uptime || 0),
      subtext: 'System running',
      icon: Clock,
      iconBg: 'bg-amber-500/10 dark:bg-amber-500/20',
      iconColor: 'text-amber-600 dark:text-amber-400',
    },
  ] : [
    {
      name: 'My Sites',
      value: sites.length,
      subtext: `${sites.filter(s => s.status === 'active').length} active`,
      icon: Globe,
      iconBg: 'bg-emerald-500/10 dark:bg-emerald-500/20',
      iconColor: 'text-emerald-600 dark:text-emerald-400',
    },
    {
      name: 'Disk Usage',
      value: formatBytes(stats?.disk_usage || 0),
      subtext: 'Used by your sites',
      icon: HardDrive,
      iconBg: 'bg-blue-500/10 dark:bg-blue-500/20',
      iconColor: 'text-blue-600 dark:text-blue-400',
    },
  ]

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Dashboard</h1>
          <p className="text-muted-foreground mt-1">Welcome back, {user?.username}</p>
        </div>
        <Link
          to="/sites/new"
          className="flex items-center gap-2 px-4 py-2.5 bg-primary hover:bg-primary/90 text-primary-foreground font-medium rounded-xl transition-colors"
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
            className="bg-card border border-border rounded-2xl p-5 hover:border-primary/20 transition-colors card-shadow"
          >
            <div className="flex items-start justify-between">
              <div>
                <p className="text-sm text-muted-foreground">{stat.name}</p>
                <p className="text-2xl font-bold mt-1">{stat.value}</p>
                <p className="text-xs text-muted-foreground mt-1">{stat.subtext}</p>
              </div>
              <div className={`w-11 h-11 rounded-xl ${stat.iconBg} flex items-center justify-center`}>
                <stat.icon className={`w-5 h-5 ${stat.iconColor}`} />
              </div>
            </div>
          </div>
        ))}
      </div>

      <div className={`grid grid-cols-1 ${isAdmin ? 'lg:grid-cols-2' : ''} gap-6`}>
        {/* Recent Sites */}
        <div className="bg-card border border-border rounded-2xl overflow-hidden card-shadow">
          <div className="flex items-center justify-between px-5 py-4 border-b border-border">
            <h2 className="font-semibold">{isAdmin ? 'Recent Sites' : 'My Sites'}</h2>
            <Link
              to="/sites"
              className="text-sm text-primary hover:underline flex items-center gap-1"
            >
              View all
              <ArrowRight className="w-4 h-4" />
            </Link>
          </div>
          <div className="divide-y divide-border">
            {sites.length === 0 ? (
              <div className="px-5 py-12 text-center">
                <div className="w-14 h-14 rounded-2xl bg-secondary flex items-center justify-center mx-auto mb-4">
                  <Globe className="w-7 h-7 text-muted-foreground" />
                </div>
                <p className="text-muted-foreground mb-2">No sites yet</p>
                <Link
                  to="/sites/new"
                  className="text-sm text-primary hover:underline"
                >
                  Create your first site
                </Link>
              </div>
            ) : (
              sites.slice(0, 5).map((site) => (
                <Link
                  key={site.id}
                  to={`/sites/${site.id}`}
                  className="flex items-center gap-4 px-5 py-4 hover:bg-secondary/50 transition-colors"
                >
                  <div className="w-10 h-10 rounded-xl bg-secondary flex items-center justify-center">
                    <Globe className="w-5 h-5 text-muted-foreground" />
                  </div>
                  <div className="flex-1 min-w-0">
                    <p className="font-medium truncate">{site.name}</p>
                    <p className="text-sm text-muted-foreground truncate">{site.domain}</p>
                  </div>
                  <span className={`text-xs px-2.5 py-1 rounded-full border font-medium ${getStatusBgColor(site.status)}`}>
                    {site.status}
                  </span>
                </Link>
              ))
            )}
          </div>
        </div>

        {/* PHP Instances - Admin only */}
        {isAdmin && (
          <div className="bg-card border border-border rounded-2xl overflow-hidden card-shadow">
            <div className="flex items-center justify-between px-5 py-4 border-b border-border">
              <h2 className="font-semibold">PHP Instances</h2>
              <Link
                to="/php"
                className="text-sm text-primary hover:underline flex items-center gap-1"
              >
                Manage
                <ArrowRight className="w-4 h-4" />
              </Link>
            </div>
            <div className="divide-y divide-border">
              {phpInstances.length === 0 ? (
                <div className="px-5 py-12 text-center">
                  <div className="w-14 h-14 rounded-2xl bg-secondary flex items-center justify-center mx-auto mb-4">
                    <Server className="w-7 h-7 text-muted-foreground" />
                  </div>
                  <p className="text-muted-foreground">No PHP instances configured</p>
                </div>
              ) : (
                phpInstances.map((instance) => (
                  <div
                    key={instance.version}
                    className="flex items-center gap-4 px-5 py-4"
                  >
                    <div className={`w-10 h-10 rounded-xl flex items-center justify-center ${
                      instance.status === 'running'
                        ? 'bg-emerald-500/10'
                        : 'bg-secondary'
                    }`}>
                      <Server className={`w-5 h-5 ${
                        instance.status === 'running'
                          ? 'text-emerald-600 dark:text-emerald-400'
                          : 'text-muted-foreground'
                      }`} />
                    </div>
                    <div className="flex-1">
                      <p className="font-medium">PHP {instance.version}</p>
                      <p className="text-sm text-muted-foreground">
                        {instance.site_count} sites • Port {instance.port}
                      </p>
                    </div>
                    <div className="flex items-center gap-3">
                      {instance.status === 'running' && (
                        <Activity className="w-4 h-4 text-emerald-500" />
                      )}
                      <span className={`text-xs px-2.5 py-1 rounded-full border font-medium ${getStatusBgColor(instance.status)}`}>
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
      <div className="bg-card border border-border rounded-2xl p-5 card-shadow">
        <h2 className="font-semibold mb-4">Quick Actions</h2>
        <div className={`grid grid-cols-2 ${isAdmin ? 'sm:grid-cols-4' : 'sm:grid-cols-2'} gap-3`}>
          <Link
            to="/sites/new"
            className="flex flex-col items-center gap-3 p-4 rounded-xl bg-secondary/50 hover:bg-secondary transition-colors"
          >
            <div className="w-10 h-10 rounded-xl bg-emerald-500/10 flex items-center justify-center">
              <Plus className="w-5 h-5 text-emerald-600 dark:text-emerald-400" />
            </div>
            <span className="text-sm font-medium">Add Site</span>
          </Link>
          <Link
            to="/sites"
            className="flex flex-col items-center gap-3 p-4 rounded-xl bg-secondary/50 hover:bg-secondary transition-colors"
          >
            <div className="w-10 h-10 rounded-xl bg-blue-500/10 flex items-center justify-center">
              <Globe className="w-5 h-5 text-blue-600 dark:text-blue-400" />
            </div>
            <span className="text-sm font-medium">My Sites</span>
          </Link>
          {isAdmin && (
            <>
              <Link
                to="/php"
                className="flex flex-col items-center gap-3 p-4 rounded-xl bg-secondary/50 hover:bg-secondary transition-colors"
              >
                <div className="w-10 h-10 rounded-xl bg-purple-500/10 flex items-center justify-center">
                  <Server className="w-5 h-5 text-purple-600 dark:text-purple-400" />
                </div>
                <span className="text-sm font-medium">PHP Manager</span>
              </Link>
              <button
                onClick={() => api.reloadAll()}
                className="flex flex-col items-center gap-3 p-4 rounded-xl bg-secondary/50 hover:bg-secondary transition-colors"
              >
                <div className="w-10 h-10 rounded-xl bg-amber-500/10 flex items-center justify-center">
                  <Zap className="w-5 h-5 text-amber-600 dark:text-amber-400" />
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
