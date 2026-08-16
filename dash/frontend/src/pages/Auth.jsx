import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { ArrowRight } from 'lucide-react'
import { useAuth } from '../lib/useAuth'

export default function Auth() {
  const [activeTab, setActiveTab] = useState('signin')
  const [form, setForm] = useState({ name: '', email: '', password: '' })
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const { login } = useAuth()
  const navigate = useNavigate()

  const set = (field) => (e) => setForm((f) => ({ ...f, [field]: e.target.value }))

  const handleSignIn = async (e) => {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      const res = await fetch('/api/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email: form.email, password: form.password }),
      })
      const data = await res.json()
      if (!res.ok) {
        if (data.error === 'account_blocked') setError('Your account has been suspended. Contact support.')
        else if (data.error === 'invalid_credentials') setError('Invalid email or password.')
        else setError(data.error || 'Sign in failed.')
        return
      }
      login(data.user, data.token)
      navigate('/dashboard')
    } catch (err) {
      setError('Could not reach server. Please try again.')
      console.error('Login error:', err)
    } finally {
      setLoading(false)
    }
  }

  const handleSignUp = async (e) => {
    e.preventDefault()
    setError('')
    if (!form.name.trim()) { setError('Name is required.'); return }
    if (form.password.length < 8) { setError('Password must be at least 8 characters.'); return }
    setLoading(true)
    try {
      const res = await fetch('/api/auth/register', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email: form.email, password: form.password, name: form.name }),
      })
      let data = {}
      try { data = await res.json() } catch { /* non-JSON response */ }
      if (!res.ok) {
        if (data.error === 'email_taken') setError('Email is already registered.')
        else if (data.error === 'password_too_short') setError('Password must be at least 8 characters.')
        else if (data.error === 'internal_error') setError('Server error. Please try again in a moment.')
        else setError(data.error || 'Registration failed.')
        return
      }
      login(data.user, data.token)
      navigate('/dashboard')
    } catch (err) {
      setError('Could not reach server. Please try again.')
      console.error('Register error:', err)
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="ahq-auth-root">
      <div className="ahq-auth-orb ahq-auth-orb-1" />
      <div className="ahq-auth-orb ahq-auth-orb-2" />

      <div className="ahq-auth-card">
        <button className="ahq-auth-back" onClick={() => navigate('/')}>
          &larr; Back
        </button>

        <div className="ahq-auth-logo">AgentHQ</div>
        <h1 className="ahq-auth-title">
          {activeTab === 'signin' ? 'Welcome back.' : 'Start for free.'}
        </h1>
        <p className="ahq-auth-sub">
          {activeTab === 'signin'
            ? 'Sign in to your agent company.'
            : '3-day trial, no credit card required.'}
        </p>

        <div className="ahq-auth-tabs">
          <button
            className={`ahq-auth-tab ${activeTab === 'signin' ? 'active' : ''}`}
            onClick={() => { setActiveTab('signin'); setError('') }}
          >Sign In</button>
          <button
            className={`ahq-auth-tab ${activeTab === 'signup' ? 'active' : ''}`}
            onClick={() => { setActiveTab('signup'); setError('') }}
          >Create Account</button>
        </div>

        {error && <div className="ahq-auth-error">{error}</div>}

        {activeTab === 'signin' ? (
          <form className="ahq-auth-form" onSubmit={handleSignIn}>
            <div className="ahq-auth-field">
              <label>Email</label>
              <input
                type="email"
                autoComplete="email"
                value={form.email}
                onChange={set('email')}
                placeholder="you@example.com"
                required
              />
            </div>
            <div className="ahq-auth-field">
              <label>Password</label>
              <input
                type="password"
                autoComplete="current-password"
                value={form.password}
                onChange={set('password')}
                placeholder="Your password"
                required
              />
            </div>
            <button className="ahq-auth-submit" type="submit" disabled={loading}>
              {loading ? <span className="ahq-auth-spinner" /> : <>Sign in <ArrowRight size={15} /></>}
            </button>
          </form>
        ) : (
          <form className="ahq-auth-form" onSubmit={handleSignUp}>
            <div className="ahq-auth-field">
              <label>Name</label>
              <input
                type="text"
                autoComplete="name"
                value={form.name}
                onChange={set('name')}
                placeholder="Your name"
                required
              />
            </div>
            <div className="ahq-auth-field">
              <label>Email</label>
              <input
                type="email"
                autoComplete="email"
                value={form.email}
                onChange={set('email')}
                placeholder="you@example.com"
                required
              />
            </div>
            <div className="ahq-auth-field">
              <label>Password</label>
              <input
                type="password"
                autoComplete="new-password"
                value={form.password}
                onChange={set('password')}
                placeholder="Min. 8 characters"
                required
                minLength={8}
              />
            </div>
            <button className="ahq-auth-submit" type="submit" disabled={loading}>
              {loading ? <span className="ahq-auth-spinner" /> : <>Create account <ArrowRight size={15} /></>}
            </button>
          </form>
        )}

        <p className="ahq-auth-terms">
          By continuing you agree to our <a href="#">Terms</a> and <a href="#">Privacy Policy</a>.
        </p>
      </div>
    </div>
  )
}
