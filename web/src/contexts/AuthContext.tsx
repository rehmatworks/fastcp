import { createContext, useContext, useState, useEffect, useCallback, ReactNode } from 'react'
import { api } from '@/lib/api'
import type { User } from '@/types'

interface AuthContextType {
  user: User | null
  realUser: User | null  // The actual logged-in admin (when impersonating)
  isAuthenticated: boolean
  isLoading: boolean
  isImpersonating: boolean
  login: (username: string, password: string) => Promise<void>
  logout: () => void
  checkAuth: () => Promise<void>
  impersonate: (username: string) => void
  stopImpersonating: () => void
}

const AuthContext = createContext<AuthContextType | undefined>(undefined)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null)
  const [realUser, setRealUser] = useState<User | null>(null)
  const [isLoading, setIsLoading] = useState(true)

  const isImpersonating = realUser !== null && user?.id !== realUser.id

  const checkAuth = useCallback(async () => {
    const token = api.getToken()
    if (!token) {
      setUser(null)
      setRealUser(null)
      setIsLoading(false)
      return
    }

    try {
      const userData = await api.getCurrentUser()
      setUser(userData as User)
      // Only set realUser if not already impersonating
      if (!realUser) {
        setRealUser(userData as User)
      }
    } catch {
      api.logout()
      setUser(null)
      setRealUser(null)
    } finally {
      setIsLoading(false)
    }
  }, [realUser])

  useEffect(() => {
    checkAuth()
  }, [])

  const login = async (username: string, password: string) => {
    const response = await api.login(username, password)
    setUser(response.user)
    setRealUser(response.user)
  }

  const logout = () => {
    api.logout()
    setUser(null)
    setRealUser(null)
  }

  const impersonate = (username: string) => {
    // Store the real user and switch to impersonated user
    // This is client-side only - API calls still use the real token
    // The backend filters data based on impersonated user header
    setUser({
      id: username,
      username: username,
      email: `${username}@localhost`,
      role: 'user', // Impersonated users are always non-admin
    })
    // Store impersonation in session
    sessionStorage.setItem('impersonating', username)
    api.setImpersonating(username)
  }

  const stopImpersonating = () => {
    if (realUser) {
      setUser(realUser)
    }
    sessionStorage.removeItem('impersonating')
    api.setImpersonating(null)
  }

  // Restore impersonation from session on load
  useEffect(() => {
    const impersonating = sessionStorage.getItem('impersonating')
    if (impersonating && realUser && realUser.role === 'admin') {
      impersonate(impersonating)
    }
  }, [realUser])

  return (
    <AuthContext.Provider
      value={{
        user,
        realUser,
        isAuthenticated: !!user,
        isLoading,
        isImpersonating,
        login,
        logout,
        checkAuth,
        impersonate,
        stopImpersonating,
      }}
    >
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth() {
  const context = useContext(AuthContext)
  if (context === undefined) {
    throw new Error('useAuth must be used within an AuthProvider')
  }
  return context
}

