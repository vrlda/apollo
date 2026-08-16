import { useState, useEffect, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '../lib/useAuth'

export default function Account() {
  const { user, token, logout, updateUser } = useAuth()
  const navigate = useNavigate()

  const [name, setName] = useState(user?.name || '')
  const [profileMsg, setProfileMsg] = useState(null)
  const [profileLoading, setProfileLoading] = useState(false)

  const [apiKey, setApiKey] = useState(user?.openrouter_api_key || '')
  const [apiKeyMsg, setApiKeyMsg] = useState(null)
  const [apiKeyLoading, setApiKeyLoading] = useState(false)

  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [passwordMsg, setPasswordMsg] = useState(null)
  const [passwordLoading, setPasswordLoading] = useState(false)

  const [deletePassword, setDeletePassword] = useState('')
  const [deleteMsg, setDeleteMsg] = useState(null)
  const [deleteLoading, setDeleteLoading] = useState(false)
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false)

  const [upgradeLoading, setUpgradeLoading] = useState(false)
  const [upgradeMsg, setUpgradeMsg] = useState(null)
  const [checkoutUrl, setCheckoutUrl] = useState(null)
  const pollRef = useRef(null)

  // Poll /api/auth/me while checkout is open to detect plan upgrade
  useEffect(() => {
    if (!checkoutUrl) {
      clearInterval(pollRef.current)
      return
    }
    pollRef.current = setInterval(async () => {
      try {
        const res = await fetch('/api/auth/me', { headers: { Authorization: `Bearer ${token}` } })
        if (!res.ok) return
        const data = await res.json()
        if (data.plan === 'pro') {
          updateUser({ plan: 'pro', subscription_ends_at: data.subscription_ends_at })
          setCheckoutUrl(null)
          setUpgradeMsg({ type: 'success', text: 'Upgrade successful! Welcome to Pro.' })
        }
      } catch { /* ignore */ }
    }, 5000)
    return () => clearInterval(pollRef.current)
  }, [checkoutUrl, token, updateUser])

  const handleSaveProfile = async (e) => {
    e.preventDefault()
    setProfileMsg(null)
    setProfileLoading(true)
    try {
      const res = await fetch('/api/auth/me', {
        method: 'PATCH',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({ name }),
      })
      const data = await res.json()
      if (!res.ok) {
        setProfileMsg({ type: 'error', text: data.error || 'Failed to update profile.' })
        return
      }
      updateUser({ name: data.name })
      setProfileMsg({ type: 'success', text: 'Profile updated.' })
    } catch {
      setProfileMsg({ type: 'error', text: 'Network error.' })
    } finally {
      setProfileLoading(false)
    }
  }

  const handleSaveApiKey = async (e) => {
    e.preventDefault()
    setApiKeyMsg(null)
    setApiKeyLoading(true)
    try {
      const res = await fetch('/api/auth/me', {
        method: 'PATCH',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({ openrouter_api_key: apiKey }),
      })
      const data = await res.json()
      if (!res.ok) {
        setApiKeyMsg({ type: 'error', text: data.error || 'Failed to save API key.' })
        return
      }
      updateUser({ openrouter_api_key: apiKey })
      setApiKeyMsg({ type: 'success', text: 'API key saved.' })
    } catch {
      setApiKeyMsg({ type: 'error', text: 'Network error.' })
    } finally {
      setApiKeyLoading(false)
    }
  }

  const handleChangePassword = async (e) => {
    e.preventDefault()
    setPasswordMsg(null)
    if (newPassword !== confirmPassword) {
      setPasswordMsg({ type: 'error', text: 'New passwords do not match.' })
      return
    }
    if (newPassword.length < 6) {
      setPasswordMsg({ type: 'error', text: 'Password must be at least 6 characters.' })
      return
    }
    setPasswordLoading(true)
    try {
      const res = await fetch('/api/auth/change-password', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }),
      })
      const data = await res.json()
      if (!res.ok) {
        setPasswordMsg({ type: 'error', text: data.error === 'invalid_current_password' ? 'Current password is incorrect.' : (data.error || 'Failed to change password.') })
        return
      }
      setCurrentPassword('')
      setNewPassword('')
      setConfirmPassword('')
      setPasswordMsg({ type: 'success', text: 'Password changed successfully.' })
    } catch {
      setPasswordMsg({ type: 'error', text: 'Network error.' })
    } finally {
      setPasswordLoading(false)
    }
  }

  const handleLogout = async () => {
    await logout()
    navigate('/auth')
  }

  const handleUpgrade = async () => {
    setUpgradeMsg(null)
    setUpgradeLoading(true)
    try {
      const res = await fetch('/api/billing/checkout', {
        method: 'POST',
        headers: { Authorization: `Bearer ${token}` },
      })
      const data = await res.json()
      if (!res.ok) {
        setUpgradeMsg({ type: 'error', text: data.error || 'Failed to start checkout.' })
        return
      }
      // Open Vulta checkout in embedded modal
      setCheckoutUrl(data.checkout_url)
    } catch {
      setUpgradeMsg({ type: 'error', text: 'Network error.' })
    } finally {
      setUpgradeLoading(false)
    }
  }

  const handleDeleteAccount = async (e) => {
    e.preventDefault()
    setDeleteMsg(null)
    setDeleteLoading(true)
    try {
      const res = await fetch('/api/auth/delete-account', {
        method: 'DELETE',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({ password: deletePassword }),
      })
      const data = await res.json()
      if (!res.ok) {
        setDeleteMsg({ type: 'error', text: data.error === 'invalid_password' ? 'Incorrect password.' : (data.error || 'Failed to delete account.') })
        return
      }
      await logout()
      navigate('/auth')
    } catch {
      setDeleteMsg({ type: 'error', text: 'Network error.' })
    } finally {
      setDeleteLoading(false)
    }
  }

  return (
    <div className="account-root">
      <button className="account-back" onClick={() => navigate('/dashboard')}>
        &larr; Back to Dashboard
      </button>
      <h1 className="account-page-title">Account</h1>
      <p className="account-page-sub">Manage your profile, security, and session.</p>

      <div className="account-content">
        {/* Profile */}
        <div className="account-card">
          <div className="account-card-title">Profile</div>
          <form onSubmit={handleSaveProfile}>
            <div className="account-field">
              <label>Display Name</label>
              <input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="Your name"
              />
            </div>
            <div className="account-field">
              <label>Email</label>
              <input type="email" value={user?.email || ''} readOnly />
            </div>
            {profileMsg && (
              <div className={`account-msg ${profileMsg.type}`}>{profileMsg.text}</div>
            )}
            <button className="account-save-btn" type="submit" disabled={profileLoading}>
              {profileLoading ? 'Saving...' : 'Save Changes'}
            </button>
          </form>
        </div>

        {/* API Keys */}
        <div className="account-card">
          <div className="account-card-title">API Keys</div>
          <p style={{ color: '#6b6b6b', fontSize: '0.875rem', marginBottom: '16px' }}>
            Your OpenRouter API key is used to run AI agents. Get one at{' '}
            <a href="https://openrouter.ai/keys" target="_blank" rel="noreferrer" style={{ color: '#b38c00' }}>
              openrouter.ai/keys
            </a>
          </p>
          <form onSubmit={handleSaveApiKey}>
            <div className="account-field">
              <label>OpenRouter API Key</label>
              <input
                type="password"
                value={apiKey}
                onChange={(e) => setApiKey(e.target.value)}
                placeholder="sk-or-..."
                autoComplete="off"
              />
            </div>
            {apiKeyMsg && (
              <div className={`account-msg ${apiKeyMsg.type}`}>{apiKeyMsg.text}</div>
            )}
            <button className="account-save-btn" type="submit" disabled={apiKeyLoading}>
              {apiKeyLoading ? 'Saving...' : 'Save API Key'}
            </button>
          </form>
        </div>

        {/* Security */}
        <div className="account-card">
          <div className="account-card-title">Security</div>
          <form onSubmit={handleChangePassword}>
            <div className="account-field">
              <label>Current Password</label>
              <input
                type="password"
                value={currentPassword}
                onChange={(e) => setCurrentPassword(e.target.value)}
                placeholder="Current password"
                required
              />
            </div>
            <div className="account-field">
              <label>New Password</label>
              <input
                type="password"
                value={newPassword}
                onChange={(e) => setNewPassword(e.target.value)}
                placeholder="New password"
                required
                minLength={6}
              />
            </div>
            <div className="account-field">
              <label>Confirm New Password</label>
              <input
                type="password"
                value={confirmPassword}
                onChange={(e) => setConfirmPassword(e.target.value)}
                placeholder="Confirm new password"
                required
              />
            </div>
            {passwordMsg && (
              <div className={`account-msg ${passwordMsg.type}`}>{passwordMsg.text}</div>
            )}
            <button className="account-save-btn" type="submit" disabled={passwordLoading}>
              {passwordLoading ? 'Updating...' : 'Change Password'}
            </button>
          </form>
        </div>

        {/* Subscription */}
        <div className="account-card">
          <div className="account-card-title">Subscription</div>
          <div className="account-plan-row">
            <span className="account-plan-badge">{user?.plan === 'pro' ? 'Pro' : 'Trial'}</span>
            <span style={{ color: '#6b6b6b', fontSize: '0.85rem' }}>
              {user?.plan === 'pro'
                ? user?.subscription_ends_at ? `Renews ${new Date(user.subscription_ends_at).toLocaleDateString()}` : 'Active'
                : user?.trial_ends_at
                  ? new Date(user.trial_ends_at) > new Date()
                    ? `Trial ends ${new Date(user.trial_ends_at).toLocaleDateString()}`
                    : 'Trial expired'
                  : 'Trial'}
            </span>
          </div>
          {user?.plan !== 'pro' ? (
            <>
              <p style={{ color: '#6b6b6b', fontSize: '0.84rem', marginBottom: '16px', lineHeight: '1.6' }}>
                Upgrade to Pro for <strong style={{ color: '#1c1c1e' }}>$39/month</strong>. Pay with crypto via Vulta — BTC, ETH, USDT, USDC, SOL, and more.
              </p>
              {upgradeMsg && (
                <div className={`account-msg ${upgradeMsg.type}`} style={{ marginBottom: '12px' }}>{upgradeMsg.text}</div>
              )}
              <button className="account-save-btn" onClick={handleUpgrade} disabled={upgradeLoading}>
                {upgradeLoading ? 'Redirecting...' : 'Upgrade to Pro →'}
              </button>
            </>
          ) : (
            <p style={{ color: '#6b6b6b', fontSize: '0.84rem', marginBottom: '16px', lineHeight: '1.6' }}>
              To cancel, contact <a href="mailto:support@agenthq.ai" style={{ color: '#1c1c1e' }}>support@agenthq.ai</a>.
            </p>
          )}
        </div>

        {/* Session */}
        <div className="account-card">
          <div className="account-card-title">Session</div>
          <p style={{ color: '#6b6b6b', fontSize: '0.875rem', marginBottom: '16px' }}>
            Signing out will remove your session token from this device.
          </p>
          <button className="account-danger-btn" onClick={handleLogout}>
            Sign Out
          </button>
        </div>

        {/* Danger zone */}
        <div className="account-card account-card-danger">
          <div className="account-card-title" style={{ color: '#dc2626', borderBottomColor: '#fca5a533' }}>Danger Zone</div>
          {!showDeleteConfirm ? (
            <>
              <p style={{ color: '#6b6b6b', fontSize: '0.875rem', marginBottom: '16px', lineHeight: '1.6' }}>
                Permanently delete your account and all associated data. This cannot be undone.
              </p>
              <button className="account-danger-btn" onClick={() => setShowDeleteConfirm(true)}>
                Delete Account
              </button>
            </>
          ) : (
            <form onSubmit={handleDeleteAccount}>
              <p style={{ color: '#dc2626', fontSize: '0.875rem', marginBottom: '16px', lineHeight: '1.6', fontWeight: 600 }}>
                Enter your password to confirm permanent deletion.
              </p>
              <div className="account-field">
                <label>Password</label>
                <input
                  type="password"
                  value={deletePassword}
                  onChange={(e) => setDeletePassword(e.target.value)}
                  placeholder="Confirm your password"
                  required
                  autoFocus
                />
              </div>
              {deleteMsg && (
                <div className={`account-msg ${deleteMsg.type}`}>{deleteMsg.text}</div>
              )}
              <div style={{ display: 'flex', gap: '10px', marginTop: '4px' }}>
                <button type="button" className="account-save-btn" onClick={() => { setShowDeleteConfirm(false); setDeletePassword(''); setDeleteMsg(null) }}>
                  Cancel
                </button>
                <button type="submit" className="account-delete-btn" disabled={deleteLoading}>
                  {deleteLoading ? 'Deleting...' : 'Yes, delete my account'}
                </button>
              </div>
            </form>
          )}
        </div>
      </div>

      {/* Vulta checkout modal */}
      {checkoutUrl && (
        <div className="checkout-overlay" onClick={() => setCheckoutUrl(null)}>
          <div className="checkout-modal" onClick={(e) => e.stopPropagation()}>
            <div className="checkout-modal-header">
              <span>Upgrade to Pro</span>
              <button className="checkout-close-btn" onClick={() => setCheckoutUrl(null)}>✕</button>
            </div>
            <iframe
              src={checkoutUrl}
              className="checkout-iframe"
              title="Checkout"
              allow="payment"
            />
          </div>
        </div>
      )}
    </div>
  )
}
