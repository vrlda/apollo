import { useState, useEffect } from 'react'
import { Users, Building2, Bot, CheckSquare, Brain, TrendingUp, Shield, Trash2, Lock, Unlock } from 'lucide-react'

function adminFetch(url, options = {}) {
  const token = sessionStorage.getItem('ahq_admin_token')
  return fetch(url, {
    ...options,
    headers: { ...(options.headers || {}), Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
  })
}

function StatCard({ icon: Icon, label, value, color }) {
  return (
    <div className="admin-stat-card">
      <div className="admin-stat-icon" style={{ background: color + '18', color }}>
        <Icon size={18} />
      </div>
      <div>
        <div className="admin-stat-value">{value ?? '—'}</div>
        <div className="admin-stat-label">{label}</div>
      </div>
    </div>
  )
}

export default function Admin() {
  const [authed, setAuthed] = useState(!!sessionStorage.getItem('ahq_admin_token'))
  const [password, setPassword] = useState('')
  const [authError, setAuthError] = useState('')
  const [stats, setStats] = useState(null)
  const [users, setUsers] = useState([])
  const [loading, setLoading] = useState(false)
  const [actionMsg, setActionMsg] = useState({})

  const handleAdminLogin = async (e) => {
    e.preventDefault()
    setAuthError('')
    // Verify the password works against /api/admin/stats
    const res = await fetch('/api/admin/stats', {
      headers: { Authorization: `Bearer ${password}` },
    })
    if (res.status === 403 || res.status === 401) {
      setAuthError('Invalid admin password.')
      return
    }
    sessionStorage.setItem('ahq_admin_token', password)
    setAuthed(true)
  }

  const load = async () => {
    setLoading(true)
    const [sRes, uRes] = await Promise.all([
      adminFetch('/api/admin/stats'),
      adminFetch('/api/admin/users'),
    ])
    if (sRes.ok) setStats(await sRes.json())
    if (uRes.ok) setUsers(await uRes.json())
    setLoading(false)
  }

  useEffect(() => {
    if (!authed) return undefined
    const timer = setTimeout(() => load(), 0)
    return () => clearTimeout(timer)
  }, [authed])

  const patchUser = async (id, body) => {
    setActionMsg((m) => ({ ...m, [id]: 'Saving...' }))
    const res = await adminFetch(`/api/admin/users/${id}`, { method: 'PATCH', body: JSON.stringify(body) })
    if (res.ok) {
      setActionMsg((m) => ({ ...m, [id]: 'Saved' }))
      load()
      setTimeout(() => setActionMsg((m) => ({ ...m, [id]: '' })), 2000)
    }
  }

  const deleteUser = async (id, email) => {
    if (!window.confirm(`Delete user ${email}? This is permanent.`)) return
    await adminFetch(`/api/admin/users/${id}`, { method: 'DELETE' })
    load()
  }

  if (!authed) {
    return (
      <div className="admin-login-root">
        <div className="admin-login-card">
          <div className="admin-login-logo">AgentHQ Admin</div>
          <p className="admin-login-sub">Enter your admin password to continue.</p>
          {authError && <div className="admin-login-error">{authError}</div>}
          <form onSubmit={handleAdminLogin} style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
            <input
              type="password"
              className="admin-login-input"
              placeholder="Admin password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoFocus
              required
            />
            <button type="submit" className="admin-login-btn">Sign in as Admin</button>
          </form>
        </div>
      </div>
    )
  }

  return (
    <div className="admin-root">
      <div className="admin-header">
        <div className="admin-header-logo">AgentHQ <span>Admin</span></div>
        <button className="admin-logout-btn" onClick={() => { sessionStorage.removeItem('ahq_admin_token'); setAuthed(false) }}>
          Sign out
        </button>
      </div>

      <div className="admin-body">
        {/* Stats */}
        <section className="admin-section">
          <h2 className="admin-section-title">Platform Overview</h2>
          <div className="admin-stats-grid">
            <StatCard icon={Users} label="Total users" value={stats?.total_users} color="#6366f1" />
            <StatCard icon={TrendingUp} label="Active trials" value={stats?.active_trials} color="#f5c53f" />
            <StatCard icon={Shield} label="Paid users" value={stats?.paid_users} color="#22c55e" />
            <StatCard icon={Lock} label="Blocked" value={stats?.blocked_users} color="#ef4444" />
            <StatCard icon={Building2} label="Companies" value={stats?.total_companies} color="#8b5cf6" />
            <StatCard icon={Bot} label="Agents" value={stats?.total_agents} color="#06b6d4" />
            <StatCard icon={CheckSquare} label="Tasks" value={stats?.total_tasks} color="#f97316" />
            <StatCard icon={Brain} label="Memories" value={stats?.total_memories} color="#ec4899" />
          </div>
        </section>

        {/* Users table */}
        <section className="admin-section">
          <h2 className="admin-section-title">Users</h2>
          {loading ? (
            <div className="admin-loading">Loading...</div>
          ) : (
            <div className="admin-table-wrap">
              <table className="admin-table">
                <thead>
                  <tr>
                    <th>User</th>
                    <th>Plan</th>
                    <th>Trial ends</th>
                    <th>Companies</th>
                    <th>Agents</th>
                    <th>Tasks</th>
                    <th>Joined</th>
                    <th>Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {users.map((u) => (
                    <tr key={u.id} className={u.is_blocked ? 'admin-row-blocked' : ''}>
                      <td>
                        <div className="admin-user-cell">
                          <div className="admin-user-name">{u.name || '—'}</div>
                          <div className="admin-user-email">{u.email}</div>
                        </div>
                      </td>
                      <td>
                        <select
                          className="admin-plan-select"
                          value={u.plan || 'trial'}
                          onChange={(e) => patchUser(u.id, { plan: e.target.value, subscription_ends_at: '' })}
                        >
                          <option value="trial">Trial</option>
                          <option value="pro">Pro</option>
                          <option value="cancelled">Cancelled</option>
                        </select>
                      </td>
                      <td className="admin-cell-muted">
                        {u.trial_ends_at ? new Date(u.trial_ends_at).toLocaleDateString() : '—'}
                      </td>
                      <td className="admin-cell-num">{u.company_count}</td>
                      <td className="admin-cell-num">{u.agent_count}</td>
                      <td className="admin-cell-num">{u.task_count}</td>
                      <td className="admin-cell-muted">
                        {new Date(u.created_at).toLocaleDateString()}
                      </td>
                      <td>
                        <div className="admin-actions">
                          {actionMsg[u.id] && <span className="admin-action-msg">{actionMsg[u.id]}</span>}
                          <button
                            className={`admin-icon-btn ${u.is_blocked ? 'admin-icon-btn-warn' : ''}`}
                            title={u.is_blocked ? 'Unblock user' : 'Block user'}
                            onClick={() => patchUser(u.id, { is_blocked: !u.is_blocked })}
                          >
                            {u.is_blocked ? <Unlock size={14} /> : <Lock size={14} />}
                          </button>
                          <button
                            className="admin-icon-btn admin-icon-btn-danger"
                            title="Delete user"
                            onClick={() => deleteUser(u.id, u.email)}
                          >
                            <Trash2 size={14} />
                          </button>
                        </div>
                      </td>
                    </tr>
                  ))}
                  {users.length === 0 && (
                    <tr><td colSpan={8} className="admin-cell-muted" style={{ textAlign: 'center', padding: '32px' }}>No users yet.</td></tr>
                  )}
                </tbody>
              </table>
            </div>
          )}
        </section>
      </div>
    </div>
  )
}
