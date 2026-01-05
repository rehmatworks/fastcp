import { useEffect, useState } from 'react'
import {
  Server,
  Play,
  Square,
  RefreshCw,
  Loader2,
  Activity,
  Cpu,
} from 'lucide-react'
import { api } from '@/lib/api'
import { formatDate, getStatusBgColor } from '@/lib/utils'
import type { PHPInstance } from '@/types'

export function PHPPage() {
  const [instances, setInstances] = useState<PHPInstance[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [actionLoading, setActionLoading] = useState<string | null>(null)

  useEffect(() => {
    fetchInstances()
  }, [])

  async function fetchInstances() {
    try {
      const data = await api.getPHPInstances()
      setInstances(data.instances || [])
    } catch (error) {
      console.error('Failed to fetch PHP instances:', error)
    } finally {
      setIsLoading(false)
    }
  }

  async function handleStart(version: string) {
    setActionLoading(version)
    try {
      await api.startPHPInstance(version)
      await fetchInstances()
    } catch (error) {
      console.error('Failed to start instance:', error)
    } finally {
      setActionLoading(null)
    }
  }

  async function handleStop(version: string) {
    setActionLoading(version)
    try {
      await api.stopPHPInstance(version)
      await fetchInstances()
    } catch (error) {
      console.error('Failed to stop instance:', error)
    } finally {
      setActionLoading(null)
    }
  }

  async function handleRestart(version: string) {
    setActionLoading(version)
    try {
      await api.restartPHPInstance(version)
      await fetchInstances()
    } catch (error) {
      console.error('Failed to restart instance:', error)
    } finally {
      setActionLoading(null)
    }
  }

  async function handleRestartWorkers(version: string) {
    setActionLoading(version)
    try {
      await api.restartPHPWorkers(version)
    } catch (error) {
      console.error('Failed to restart workers:', error)
    } finally {
      setActionLoading(null)
    }
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
        <h1 className="text-2xl font-bold">PHP Instances</h1>
        <p className="text-muted-foreground">
          Manage FrankenPHP instances for different PHP versions
        </p>
      </div>

      {/* Instances Grid */}
      <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
        {instances.map((instance) => (
          <div
            key={instance.version}
            className="bg-card border border-border rounded-xl overflow-hidden hover:border-emerald-500/30 transition-colors"
          >
            {/* Header */}
            <div className="px-5 py-4 border-b border-border bg-secondary/30">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-3">
                  <div
                    className={`w-10 h-10 rounded-lg flex items-center justify-center ${
                      instance.status === 'running'
                        ? 'bg-emerald-500/20 border border-emerald-500/30'
                        : 'bg-secondary border border-border'
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
                  <div>
                    <h3 className="font-semibold">PHP {instance.version}</h3>
                    <p className="text-xs text-muted-foreground">
                      Port {instance.port}
                    </p>
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  {instance.status === 'running' && (
                    <Activity className="w-4 h-4 text-emerald-400 animate-pulse" />
                  )}
                  <span
                    className={`text-xs px-2.5 py-1 rounded-full border ${getStatusBgColor(
                      instance.status
                    )}`}
                  >
                    {instance.status}
                  </span>
                </div>
              </div>
            </div>

            {/* Stats */}
            <div className="px-5 py-4 space-y-3">
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <p className="text-xs text-muted-foreground">Sites</p>
                  <p className="text-lg font-semibold">{instance.site_count}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">Threads</p>
                  <p className="text-lg font-semibold">
                    {instance.thread_count || '-'}
                  </p>
                </div>
              </div>

              {instance.status === 'running' && instance.started_at && (
                <div className="pt-3 border-t border-border">
                  <p className="text-xs text-muted-foreground">Started</p>
                  <p className="text-sm">{formatDate(instance.started_at)}</p>
                </div>
              )}
            </div>

            {/* Actions */}
            <div className="px-5 py-4 border-t border-border bg-secondary/20">
              <div className="flex items-center gap-2">
                {instance.status === 'running' ? (
                  <>
                    <button
                      onClick={() => handleStop(instance.version)}
                      disabled={actionLoading === instance.version}
                      className="flex-1 flex items-center justify-center gap-2 px-3 py-2 text-sm bg-secondary hover:bg-secondary/80 rounded-lg transition-colors disabled:opacity-50"
                    >
                      {actionLoading === instance.version ? (
                        <Loader2 className="w-4 h-4 animate-spin" />
                      ) : (
                        <Square className="w-4 h-4" />
                      )}
                      Stop
                    </button>
                    <button
                      onClick={() => handleRestart(instance.version)}
                      disabled={actionLoading === instance.version}
                      className="flex-1 flex items-center justify-center gap-2 px-3 py-2 text-sm bg-secondary hover:bg-secondary/80 rounded-lg transition-colors disabled:opacity-50"
                    >
                      <RefreshCw className="w-4 h-4" />
                      Restart
                    </button>
                  </>
                ) : (
                  <button
                    onClick={() => handleStart(instance.version)}
                    disabled={actionLoading === instance.version}
                    className="flex-1 flex items-center justify-center gap-2 px-3 py-2 text-sm bg-emerald-500/20 text-emerald-400 hover:bg-emerald-500/30 rounded-lg transition-colors disabled:opacity-50"
                  >
                    {actionLoading === instance.version ? (
                      <Loader2 className="w-4 h-4 animate-spin" />
                    ) : (
                      <Play className="w-4 h-4" />
                    )}
                    Start
                  </button>
                )}
              </div>

              {instance.status === 'running' && (
                <button
                  onClick={() => handleRestartWorkers(instance.version)}
                  disabled={actionLoading === instance.version}
                  className="w-full mt-2 flex items-center justify-center gap-2 px-3 py-2 text-sm text-muted-foreground hover:text-foreground hover:bg-secondary rounded-lg transition-colors"
                >
                  <Cpu className="w-4 h-4" />
                  Restart Workers
                </button>
              )}
            </div>
          </div>
        ))}
      </div>

      {instances.length === 0 && (
        <div className="bg-card border border-border rounded-xl p-12 text-center">
          <Server className="w-12 h-12 mx-auto mb-4 text-muted-foreground opacity-50" />
          <h3 className="font-semibold text-lg mb-2">No PHP instances configured</h3>
          <p className="text-muted-foreground">
            Configure PHP versions in the settings to get started
          </p>
        </div>
      )}

      {/* Info Card */}
      <div className="bg-gradient-to-r from-emerald-500/10 to-emerald-600/10 border border-emerald-500/20 rounded-xl p-5">
        <div className="flex items-start gap-4">
          <div className="w-10 h-10 rounded-lg bg-emerald-500/20 flex items-center justify-center flex-shrink-0">
            <Server className="w-5 h-5 text-emerald-400" />
          </div>
          <div>
            <h3 className="font-semibold mb-1">About PHP Instances</h3>
            <p className="text-sm text-muted-foreground">
              Each PHP version runs as a separate FrankenPHP process. Sites are
              automatically routed to the correct instance based on their PHP version
              setting. Worker mode keeps your applications in memory for maximum
              performance.
            </p>
          </div>
        </div>
      </div>
    </div>
  )
}

