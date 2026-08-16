import { useState } from 'react'
import { AuthContext } from './auth-context'

export function AuthProvider({ children }) {
  const [user, setUser] = useState(() => {
    const stored = localStorage.getItem('agenthq_user')
    return stored ? JSON.parse(stored) : null
  })
  const [token, setToken] = useState(() => localStorage.getItem('agenthq_token') || null)

  const login = (userData, tokenValue) => {
    setUser(userData)
    setToken(tokenValue)
    localStorage.setItem('agenthq_user', JSON.stringify(userData))
    localStorage.setItem('agenthq_token', tokenValue)
  }

  const logout = async () => {
    if (token) {
      try {
        await fetch('/api/auth/logout', {
          method: 'POST',
          headers: { Authorization: `Bearer ${token}` },
        })
      } catch {
        // A failed logout request should not keep the local session alive.
      }
    }
    setUser(null)
    setToken(null)
    localStorage.removeItem('agenthq_user')
    localStorage.removeItem('agenthq_token')
  }

  const updateUser = (updates) => {
    const updated = { ...user, ...updates }
    setUser(updated)
    localStorage.setItem('agenthq_user', JSON.stringify(updated))
  }

  return (
    <AuthContext.Provider value={{ user, token, login, logout, updateUser }}>
      {children}
    </AuthContext.Provider>
  )
}
