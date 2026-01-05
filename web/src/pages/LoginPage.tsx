import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Eye, EyeOff, Loader2, Zap, ArrowRight } from 'lucide-react'
import { useAuth } from '@/hooks/useAuth'

export function LoginPage() {
  const navigate = useNavigate()
  const { login, isAuthenticated } = useAuth()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [showPassword, setShowPassword] = useState(false)
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState('')

  // Redirect if already authenticated
  if (isAuthenticated) {
    navigate('/', { replace: true })
    return null
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setIsLoading(true)

    try {
      await login(username, password)
      navigate('/')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Login failed')
    } finally {
      setIsLoading(false)
    }
  }

  return (
    <div className="min-h-screen bg-background flex items-center justify-center p-4 relative overflow-hidden">
      {/* Animated background */}
      <div className="absolute inset-0">
        {/* Main gradient orbs */}
        <div className="absolute top-0 right-0 w-[800px] h-[800px] bg-gradient-to-bl from-emerald-500/20 via-teal-500/10 to-transparent rounded-full blur-3xl animate-float" />
        <div className="absolute bottom-0 left-0 w-[600px] h-[600px] bg-gradient-to-tr from-blue-500/10 via-indigo-500/5 to-transparent rounded-full blur-3xl" style={{ animationDelay: '2s' }} />
        <div className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-[400px] h-[400px] bg-gradient-to-r from-emerald-500/5 to-cyan-500/5 rounded-full blur-3xl" />
        
        {/* Grid pattern */}
        <div 
          className="absolute inset-0 opacity-[0.02]"
          style={{
            backgroundImage: `linear-gradient(rgba(255,255,255,0.1) 1px, transparent 1px), linear-gradient(90deg, rgba(255,255,255,0.1) 1px, transparent 1px)`,
            backgroundSize: '64px 64px',
          }}
        />
      </div>

      <div className="relative w-full max-w-md">
        {/* Logo */}
        <div className="flex flex-col items-center mb-10">
          <div className="relative">
            <div className="w-20 h-20 rounded-2xl bg-gradient-to-br from-emerald-400 via-emerald-500 to-teal-600 flex items-center justify-center shadow-2xl shadow-emerald-500/30 mb-5">
              <Zap className="w-10 h-10 text-white" />
            </div>
            <div className="absolute -inset-1 bg-gradient-to-br from-emerald-400 to-teal-600 rounded-2xl blur opacity-30 -z-10" />
          </div>
          <h1 className="text-3xl font-bold tracking-tight">FastCP</h1>
          <p className="text-muted-foreground mt-2">Sign in to your control panel</p>
        </div>

        {/* Login form */}
        <div className="relative">
          {/* Glass card effect */}
          <div className="absolute -inset-px bg-gradient-to-b from-white/[0.08] to-white/[0.02] rounded-2xl" />
          <div className="relative bg-card/80 backdrop-blur-xl border border-white/[0.08] rounded-2xl p-8 shadow-2xl shadow-black/20">
            <form onSubmit={handleSubmit} className="space-y-6">
              {error && (
                <div className="flex items-center gap-3 bg-red-500/10 border border-red-500/20 text-red-400 px-4 py-3 rounded-xl text-sm animate-fade-in">
                  <div className="w-2 h-2 rounded-full bg-red-400 flex-shrink-0" />
                  {error}
                </div>
              )}

              <div className="space-y-2">
                <label htmlFor="username" className="block text-sm font-medium text-foreground/80">
                  Username
                </label>
                <input
                  id="username"
                  type="text"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  className="w-full px-4 py-3 bg-white/[0.03] border border-white/[0.08] rounded-xl focus:outline-none focus:ring-2 focus:ring-emerald-500/50 focus:border-emerald-500/50 transition-all duration-200 placeholder:text-muted-foreground/50"
                  placeholder="Enter your username"
                  required
                  autoComplete="username"
                />
              </div>

              <div className="space-y-2">
                <label htmlFor="password" className="block text-sm font-medium text-foreground/80">
                  Password
                </label>
                <div className="relative">
                  <input
                    id="password"
                    type={showPassword ? 'text' : 'password'}
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    className="w-full px-4 py-3 pr-12 bg-white/[0.03] border border-white/[0.08] rounded-xl focus:outline-none focus:ring-2 focus:ring-emerald-500/50 focus:border-emerald-500/50 transition-all duration-200 placeholder:text-muted-foreground/50"
                    placeholder="Enter your password"
                    required
                    autoComplete="current-password"
                  />
                  <button
                    type="button"
                    onClick={() => setShowPassword(!showPassword)}
                    className="absolute right-3 top-1/2 -translate-y-1/2 p-1.5 text-muted-foreground hover:text-foreground hover:bg-white/[0.05] rounded-lg transition-all"
                  >
                    {showPassword ? (
                      <EyeOff className="w-4 h-4" />
                    ) : (
                      <Eye className="w-4 h-4" />
                    )}
                  </button>
                </div>
              </div>

              <button
                type="submit"
                disabled={isLoading}
                className="w-full py-3.5 px-4 bg-gradient-to-r from-emerald-500 to-teal-600 hover:from-emerald-600 hover:to-teal-700 text-white font-semibold rounded-xl transition-all duration-200 disabled:opacity-50 disabled:cursor-not-allowed flex items-center justify-center gap-2 shadow-lg shadow-emerald-500/25 btn-lift group"
              >
                {isLoading ? (
                  <>
                    <Loader2 className="w-5 h-5 animate-spin" />
                    Signing in...
                  </>
                ) : (
                  <>
                    Sign In
                    <ArrowRight className="w-4 h-4 group-hover:translate-x-0.5 transition-transform" />
                  </>
                )}
              </button>
            </form>

            <div className="mt-8 pt-6 border-t border-white/[0.06]">
              <div className="flex items-center justify-center gap-2 text-xs text-muted-foreground">
                <span>Default:</span>
                <code className="px-2 py-1 bg-white/[0.05] rounded-md font-mono">admin</code>
                <span>/</span>
                <code className="px-2 py-1 bg-white/[0.05] rounded-md font-mono">fastcp2024!</code>
              </div>
            </div>
          </div>
        </div>

        <p className="text-center text-xs text-muted-foreground/60 mt-8">
          Modern PHP Hosting Control Panel
        </p>
      </div>
    </div>
  )
}
