import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { Building2, Users, Bot, CheckSquare, Calendar, Brain, Shield, Settings as SettingsIcon, LogOut, User, X } from 'lucide-react'
import AgentOSShell from '../components/AgentOSShell'
import Settings from '../components/Settings'
import { useAuth } from '../lib/useAuth'

const NAV_ITEMS = [
  { tab: 'companies', icon: Building2, label: 'Org' },
  { tab: 'departments', icon: Users, label: 'Depts' },
  { tab: 'agents', icon: Bot, label: 'Agents' },
  { tab: 'tasks', icon: CheckSquare, label: 'Tasks' },
  { tab: 'calendar', icon: Calendar, label: 'Schedule' },
  { tab: 'memory', icon: Brain, label: 'Memory' },
  { tab: 'governance', icon: Shield, label: 'Policy' },
]

function useTrialBanner(user) {
  if (!user?.trial_ends_at) return null
  const endsAt = new Date(user.trial_ends_at)
  const now = new Date()
  const diffMs = endsAt - now
  if (diffMs > 0) {
    const daysLeft = Math.ceil(diffMs / (1000 * 60 * 60 * 24))
    return { expired: false, daysLeft }
  }
  return { expired: true, daysLeft: 0 }
}

const ONBOARDING_KEY = 'ahq_onboarding_done'

function OnboardingModal({ onClose }) {
  return (
    <div className="ahq-onboarding-overlay">
      <div className="ahq-onboarding-modal">
        <button className="ahq-onboarding-close" onClick={onClose}><X size={16} /></button>
        <div className="ahq-onboarding-badge">Welcome</div>
        <h2>Your agent company is ready.</h2>
        <p>We created a demo company so you can see how everything fits together. Here's how to get started:</p>
        <ol className="ahq-onboarding-steps">
          <li>
            <span>1</span>
            <div><strong>Explore the Org tab</strong> — your demo company, department, and agent "Alex" are already there.</div>
          </li>
          <li>
            <span>2</span>
            <div><strong>Add your OpenRouter key</strong> in <em>Account → API Keys</em> so agents can run AI tasks.</div>
          </li>
          <li>
            <span>3</span>
            <div><strong>Create a task</strong> for Alex in the Agents tab, or start a new company from scratch.</div>
          </li>
        </ol>
        <button className="ahq-onboarding-btn" onClick={onClose}>Got it, let's go</button>
      </div>
    </div>
  )
}

export default function Dashboard() {
  const [showSettings, setShowSettings] = useState(false)
  const [activeTab, setActiveTab] = useState('companies')
  const [showOnboarding, setShowOnboarding] = useState(false)
  const { user, logout } = useAuth()
  const navigate = useNavigate()
  const trial = useTrialBanner(user)

  useEffect(() => {
    if (!localStorage.getItem(ONBOARDING_KEY)) {
      const timer = setTimeout(() => setShowOnboarding(true), 800)
      return () => clearTimeout(timer)
    }
  }, [])

  const dismissOnboarding = () => {
    localStorage.setItem(ONBOARDING_KEY, '1')
    setShowOnboarding(false)
  }

  const handleLogout = async () => {
    await logout()
    navigate('/auth')
  }

  return (
    <div className="ahq-app">
      <nav className="ahq-nav">
        <div className="ahq-nav-logo">AHQ</div>
        {NAV_ITEMS.map(({ tab, icon: Icon, label }) => (
          <button
            key={tab}
            className={`ahq-nav-item ${activeTab === tab ? 'active' : ''}`}
            type="button"
            onClick={() => setActiveTab(tab)}
            title={label}
          >
            <Icon size={18} />
            <span>{label}</span>
          </button>
        ))}
        <div className="ahq-nav-spacer" />
        <div className="ahq-nav-bottom">
          <button className="ahq-nav-icon-btn" type="button" title={user?.name || user?.email || 'Account'} onClick={() => navigate('/account')}>
            <User size={18} />
          </button>
          <button className="ahq-nav-icon-btn" type="button" title="Settings" onClick={() => setShowSettings(true)}>
            <SettingsIcon size={18} />
          </button>
          <button className="ahq-nav-icon-btn" type="button" title="Logout" onClick={handleLogout}>
            <LogOut size={18} />
          </button>
        </div>
      </nav>
      <div className="ahq-content">
        {trial?.expired && (
          <div className="ahq-trial-banner ahq-trial-expired">
            <span>Your 3-day trial has ended.</span>
            <button onClick={() => navigate('/account')}>Upgrade to continue &rarr;</button>
          </div>
        )}
        {trial && !trial.expired && trial.daysLeft <= 3 && (
          <div className="ahq-trial-banner ahq-trial-active">
            <span>{trial.daysLeft === 1 ? 'Last day of trial.' : `${trial.daysLeft} days left in trial.`}</span>
            <button onClick={() => navigate('/account')}>View plans &rarr;</button>
          </div>
        )}
        <AgentOSShell activeTab={activeTab} setActiveTab={setActiveTab} />
      </div>
      {showSettings && <Settings onClose={() => setShowSettings(false)} />}
      {showOnboarding && <OnboardingModal onClose={dismissOnboarding} />}
    </div>
  )
}
