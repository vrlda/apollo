import { useNavigate } from 'react-router-dom'
import { Building2, Layers, Plug, Shield, Calendar, Brain, ArrowRight, Check } from 'lucide-react'

const features = [
  {
    icon: Building2,
    title: 'Real org structure',
    desc: 'Companies, departments, managers, workers. Your AI workforce mirrors how real businesses operate.',
  },
  {
    icon: Layers,
    title: '350+ models via OpenRouter',
    desc: 'Claude, GPT-4, Mistral, Llama — any model, any time. You pick, you control costs entirely.',
  },
  {
    icon: Plug,
    title: 'MCP integration',
    desc: 'Connect any tool via Model Context Protocol. Databases, APIs, browsers, file systems — all wired in.',
  },
  {
    icon: Shield,
    title: 'Governance policies',
    desc: 'Define exactly what agents can and can\'t do. Approval gates, policy rules, tamper-proof audit logs.',
  },
  {
    icon: Calendar,
    title: 'Calendar triggers',
    desc: 'Schedule autonomous operations. Agents run at 9am, process data overnight, report every Friday.',
  },
  {
    icon: Brain,
    title: 'Persistent memory',
    desc: 'Every action, every decision, every output — logged with timestamps. Your agent company has a brain.',
  },
]

const manifestoItems = [
  {
    label: 'Agents that own outcomes',
    body: 'Not chatbots. Not copilots. Workers with roles, responsibilities, and memory — running autonomously while you sleep.',
  },
  {
    label: 'You bring the models',
    body: 'Use your own OpenRouter key. Pick any model for any role. We never mark up your AI costs.',
  },
  {
    label: 'Infrastructure, not a marketplace',
    body: 'AgentHQ is a platform. Not an app store. You build, you own, you control every agent that works under your roof.',
  },
  {
    label: 'Governance built in',
    body: 'Every action is logged. Every policy is enforced. Every output is auditable. AI that operates with accountability.',
  },
]

const included = [
  'Unlimited agents, companies, departments',
  '350+ models via OpenRouter (your key)',
  'MCP tool integrations',
  'Governance policies & audit logs',
  'Calendar-triggered operations',
  'Persistent memory system',
]

export default function Landing() {
  const navigate = useNavigate()

  return (
    <div className="landing-root">
      <nav className="landing-nav">
        <span className="landing-nav-logo">AgentHQ</span>
        <button className="landing-nav-cta" onClick={() => navigate('/auth')}>Sign In</button>
      </nav>

      {/* Hero */}
      <section className="landing-hero">
        <div className="landing-orb landing-orb-1" />
        <div className="landing-orb landing-orb-2" />
        <div className="landing-orb landing-orb-3" />
        <div className="landing-hero-badge">Free to start &middot; No credit card</div>
        <h1>Build a company<br />that runs itself.</h1>
        <p className="landing-hero-sub">
          Hire AI agents. Assign them departments. Give them memory, tools, and authority.
          Watch them work — around the clock, without you.
        </p>
        <div className="landing-cta-row">
          <button className="landing-btn-primary" onClick={() => navigate('/auth')}>
            Start for free <ArrowRight size={16} />
          </button>
        </div>
        <p className="landing-cta-sub">3-day free trial &middot; No credit card required</p>
      </section>

      {/* Manifesto */}
      <section className="landing-manifesto">
        <div className="landing-manifesto-header">
          <span className="landing-manifesto-eyebrow">How it works</span>
          <h2>A new kind of workforce.</h2>
        </div>
        <div className="landing-manifesto-grid">
          {manifestoItems.map((item, i) => (
            <div key={i} className="landing-manifesto-item">
              <div className="landing-manifesto-num">0{i + 1}</div>
              <div>
                <h3>{item.label}</h3>
                <p>{item.body}</p>
              </div>
            </div>
          ))}
        </div>
      </section>

      {/* Features */}
      <section className="landing-features">
        <h2>Everything a company needs.<br />Run by agents.</h2>
        <div className="landing-features-grid">
          {features.map(({ icon: Icon, title, desc }) => (
            <div key={title} className="landing-feature-card">
              <div className="landing-feature-icon">
                <Icon size={20} />
              </div>
              <h3>{title}</h3>
              <p>{desc}</p>
            </div>
          ))}
        </div>
      </section>

      {/* Final CTA */}
      <section className="landing-final-cta">
        <div className="landing-final-cta-inner">
          <h2>Start a company tonight.</h2>
          <p>One account. Unlimited agents. Your org, running on autopilot.</p>
          <ul className="landing-final-included">
            {included.map((item) => (
              <li key={item}>
                <Check size={14} />
                {item}
              </li>
            ))}
          </ul>
          <button className="landing-btn-primary" onClick={() => navigate('/auth')}>
            Get started free <ArrowRight size={16} />
          </button>
          <p className="landing-cta-sub" style={{ marginTop: '12px' }}>
            3-day free trial &middot; No contracts &middot; Cancel anytime
          </p>
        </div>
      </section>

      <footer className="landing-footer">
        &copy; 2025 AgentHQ. All rights reserved.
      </footer>
    </div>
  )
}
