import { useEffect, useMemo, useState } from 'react'
import { Activity, Bot, Brain, CalendarDays, CheckCircle2, MessageSquare, Plus, ShieldCheck, Sparkles } from 'lucide-react'

function isNotFoundError(err) {
  const message = String(err?.message || '')
  return message.startsWith('404') || message.toLowerCase().includes('404')
}

async function api(path, options = {}) {
  const token = localStorage.getItem('agenthq_token')
  const headers = {
    ...(options.headers || {}),
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
  }
  const res = await fetch(path, { ...options, headers })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(`${res.status} ${text || res.statusText || `HTTP ${res.status}`}`.trim())
  }
  return res.json()
}

async function apiOrDefault(path, fallbackValue) {
  try {
    return await api(path)
  } catch (err) {
    if (isNotFoundError(err)) {
      return fallbackValue
    }
    throw new Error(`${path}: ${err.message}`)
  }
}

function EmptyAction({ title, body, action, onClick, disabled = false }) {
  return (
    <div className="agentos-empty-action">
      <div className="agentos-empty-icon"><Sparkles size={18} /></div>
      <div>
        <strong>{title}</strong>
        <span>{body}</span>
      </div>
      {action && (
        <button className="btn-primary" type="button" onClick={onClick} disabled={disabled}>
          <Plus size={15} /> {action}
        </button>
      )}
    </div>
  )
}

function OverviewMetric({ icon: Icon, label, value, tone = 'neutral' }) {
  return (
    <div className={`ahq-metric-card tone-${tone}`}>
      <div className="ahq-metric-icon"><Icon size={18} /></div>
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  )
}

export default function AgentOSShell({ activeTab, setActiveTab }) {
  const [companies, setCompanies] = useState([])
  const [departments, setDepartments] = useState([])
  const [agents, setAgents] = useState([])
  const [tasks, setTasks] = useState([])

  const [threads, setThreads] = useState([])
  const [messages, setMessages] = useState([])
  const [schedules, setSchedules] = useState([])
  const [events, setEvents] = useState([])
  const [memoryTimeline, setMemoryTimeline] = useState([])
  const [memoryHits, setMemoryHits] = useState([])
  const [policies, setPolicies] = useState([])
  const [approvals, setApprovals] = useState([])
  const [auditVerify, setAuditVerify] = useState(null)

  const [modelProfiles, setModelProfiles] = useState([])
  const [selectedProfileId, setSelectedProfileId] = useState('')

  const [selectedCompanyId, setSelectedCompanyId] = useState('')
  const [selectedDepartmentId, setSelectedDepartmentId] = useState('')
  const [selectedAgentId, setSelectedAgentId] = useState('')
  const [selectedThreadId, setSelectedThreadId] = useState('')

  const [chatInput, setChatInput] = useState('')
  const [newThreadTitle, setNewThreadTitle] = useState('')
  const [memoryQuery, setMemoryQuery] = useState('')
  const [scheduleExpr, setScheduleExpr] = useState('*/30 * * * *')
const [scheduleMode, setScheduleMode] = useState('cron')
const [scheduleOnce, setScheduleOnce] = useState('')
const [scheduleMessage, setScheduleMessage] = useState('')
const [interAgentInbox, setInterAgentInbox] = useState([])
const [interAgentTo, setInterAgentTo] = useState('')
const [interAgentContent, setInterAgentContent] = useState('')
  const [statusMessage, setStatusMessage] = useState('')
  const [lastEventId, setLastEventId] = useState(0)

  const [companyWorkspacePath, setCompanyWorkspacePath] = useState('')
  const [companyDeployCommand, setCompanyDeployCommand] = useState('')

  const [modalType, setModalType] = useState('')
  const [modalCompanyId, setModalCompanyId] = useState('')
  const [modalDepartmentId, setModalDepartmentId] = useState('')

  const [newCompanyName, setNewCompanyName] = useState('')
  const [newCompanyPath, setNewCompanyPath] = useState('')
  const [newCompanyDeployCommand, setNewCompanyDeployCommand] = useState('')
  const [newDepartmentName, setNewDepartmentName] = useState('')
  const [newAgentName, setNewAgentName] = useState('')
  const [newAgentRole, setNewAgentRole] = useState('worker')

  const companyByID = useMemo(() => {
    const map = {}
    for (const company of companies) {
      map[company.id] = company
    }
    return map
  }, [companies])

  const departmentByID = useMemo(() => {
    const map = {}
    for (const department of departments) {
      map[department.id] = department
    }
    return map
  }, [departments])

  const agentByID = useMemo(() => {
    const map = {}
    for (const agent of agents) {
      map[agent.id] = agent
    }
    return map
  }, [agents])

  const selectedCompany = useMemo(
    () => companies.find((c) => c.id === selectedCompanyId) || null,
    [companies, selectedCompanyId],
  )

  const selectedAgent = useMemo(
    () => agents.find((a) => a.id === selectedAgentId) || null,
    [agents, selectedAgentId],
  )

  const agentsByDepartment = useMemo(() => {
    const grouped = {}
    for (const agent of agents) {
      if (!grouped[agent.department_id]) {
        grouped[agent.department_id] = []
      }
      grouped[agent.department_id].push(agent)
    }
    return grouped
  }, [agents])

  const departmentsByCompany = useMemo(() => {
    const grouped = {}
    for (const department of departments) {
      if (!grouped[department.company_id]) {
        grouped[department.company_id] = []
      }
      grouped[department.company_id].push(department)
    }
    return grouped
  }, [departments])

  const agentsByCompany = useMemo(() => {
    const grouped = {}
    for (const agent of agents) {
      if (!grouped[agent.company_id]) {
        grouped[agent.company_id] = []
      }
      grouped[agent.company_id].push(agent)
    }
    return grouped
  }, [agents])

  const tasksByCompany = useMemo(() => {
    const grouped = {}
    for (const task of tasks) {
      const companyID = task.company_id || agentByID[task.agent_id]?.company_id || ''
      if (!companyID) continue
      if (!grouped[companyID]) {
        grouped[companyID] = []
      }
      grouped[companyID].push(task)
    }
    return grouped
  }, [tasks, agentByID])

  const tasksByDepartment = useMemo(() => {
    const grouped = {}
    for (const task of tasks) {
      const departmentID = task.department_id || agentByID[task.agent_id]?.department_id || ''
      if (!departmentID) continue
      if (!grouped[departmentID]) {
        grouped[departmentID] = []
      }
      grouped[departmentID].push(task)
    }
    return grouped
  }, [tasks, agentByID])

  const scheduleEvents = useMemo(
    () => events.filter((e) => String(e?.event_type || '').toLowerCase().includes('schedule')).slice(-16).reverse(),
    [events],
  )

  const runningTasks = useMemo(() => tasks.filter((task) => task.status === 'running'), [tasks])
  const activeSchedules = useMemo(() => schedules.filter((schedule) => schedule.is_active), [schedules])
  const departmentsInModalCompany = useMemo(
    () => departments.filter((department) => department.company_id === modalCompanyId),
    [departments, modalCompanyId],
  )

  const loadCompanies = async () => {
    try {
      const data = await api('/api/companies')
      setCompanies(data || [])
    } catch (err) {
      if (!isNotFoundError(err)) {
        throw err
      }
      setCompanies([])
    }
  }

  const loadDirectoryData = async () => {
    const [deps, ags, tks] = await Promise.all([
      apiOrDefault('/api/departments', []),
      apiOrDefault('/api/agents', []),
      apiOrDefault('/api/tasks?limit=300', []),
    ])
    setDepartments(deps || [])
    setAgents(ags || [])
    setTasks(tks || [])
  }

  const loadModelProfiles = async () => {
    try {
      const data = await api('/api/model-profiles')
      setModelProfiles(data || [])
      if (data?.length && !selectedProfileId) {
        setSelectedProfileId(data[0].id)
      }
    } catch (err) {
      if (!isNotFoundError(err)) {
        throw err
      }
      setModelProfiles([])
    }
  }

  const loadCompanyRuntimeData = async (companyID) => {
    if (!companyID) {
      setSchedules([])
      setMemoryTimeline([])
      setEvents([])
      setLastEventId(0)
      return
    }
    const [sch, mem, evs] = await Promise.all([
      api(`/api/schedules?company_id=${encodeURIComponent(companyID)}`),
      api(`/api/memory/timeline?company_id=${encodeURIComponent(companyID)}&limit=80`),
      api(`/api/events?company_id=${encodeURIComponent(companyID)}&since_id=0&limit=120`),
    ])
    setSchedules(sch || [])
    setMemoryTimeline(mem || [])
    setEvents(evs || [])
    const maxID = (evs || []).reduce((maxValue, item) => Math.max(maxValue, item?.id || 0), 0)
    setLastEventId(maxID)
  }

  const loadThreads = async (agentID) => {
    if (!agentID) {
      setThreads([])
      setSelectedThreadId('')
      setMessages([])
      return
    }
    const data = await api(`/api/threads?agent_id=${encodeURIComponent(agentID)}`)
    setThreads(data || [])
    if (data?.length) {
      setSelectedThreadId((previousID) => (previousID && data.find((item) => item.id === previousID) ? previousID : data[0].id))
    } else {
      setSelectedThreadId('')
      setMessages([])
    }
  }

  const loadMessages = async (threadID) => {
    if (!threadID) {
      setMessages([])
      return
    }
    const data = await api(`/api/threads/${encodeURIComponent(threadID)}/messages`)
    setMessages(data || [])
  }

  const loadGovernance = async () => {
    if (!selectedCompanyId) {
      setPolicies([])
      setApprovals([])
      setAuditVerify(null)
      return
    }
    const [ps, aps, verify] = await Promise.allSettled([
      api(`/api/policies?company_id=${encodeURIComponent(selectedCompanyId)}`),
      api(`/api/approvals?company_id=${encodeURIComponent(selectedCompanyId)}&status=pending`),
      api('/api/audit/verify'),
    ])
    setPolicies(ps.status === 'fulfilled' ? (ps.value || []) : [])
    setApprovals(aps.status === 'fulfilled' ? (aps.value || []) : [])
    setAuditVerify(verify.status === 'fulfilled' ? (verify.value || null) : null)
  }

  async function loadAgentInbox(agentId, companyId) {
    if (!agentId || !companyId) return
    const msgs = await api(`/api/inter-agent?agent_id=${encodeURIComponent(agentId)}&company_id=${encodeURIComponent(companyId)}`)
    setInterAgentInbox(msgs || [])
  }

  useEffect(() => {
    Promise.allSettled([loadCompanies(), loadDirectoryData(), loadModelProfiles()]).then((results) => {
      const failures = results
        .filter((result) => result.status === 'rejected')
        .map((result) => result.reason)
      if (failures.length === 0) {
        return
      }
      const non404 = failures.filter((err) => !isNotFoundError(err))
      if (non404.length === 0) {
        setStatusMessage('Some Agent OS endpoints are not available yet. Rebuild/restart backend to enable full features.')
        return
      }
      setStatusMessage(`Failed to load bootstrap data: ${non404[0]?.message || non404[0]}`)
    })
    // Bootstrap helpers intentionally run once on mount.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    if (companies.length === 0) {
      if (selectedCompanyId) setSelectedCompanyId('')
      if (selectedDepartmentId) setSelectedDepartmentId('')
      if (selectedAgentId) setSelectedAgentId('')
      return
    }

    if (!companies.some((company) => company.id === selectedCompanyId)) {
      setSelectedCompanyId(companies[0].id)
      return
    }

    const scopedDepartments = departments.filter((department) => department.company_id === selectedCompanyId)
    if (scopedDepartments.length === 0) {
      if (selectedDepartmentId) setSelectedDepartmentId('')
      if (selectedAgentId) setSelectedAgentId('')
      return
    }

    if (!scopedDepartments.some((department) => department.id === selectedDepartmentId)) {
      setSelectedDepartmentId(scopedDepartments[0].id)
      return
    }

    const scopedAgents = agents.filter((agent) => agent.department_id === selectedDepartmentId)
    if (scopedAgents.length === 0) {
      if (selectedAgentId) setSelectedAgentId('')
      return
    }

    if (!scopedAgents.some((agent) => agent.id === selectedAgentId)) {
      setSelectedAgentId(scopedAgents[0].id)
    }
  }, [companies, departments, agents, selectedCompanyId, selectedDepartmentId, selectedAgentId])

  useEffect(() => {
    loadCompanyRuntimeData(selectedCompanyId).catch((err) => setStatusMessage(`Failed to load company data: ${err.message}`))
  }, [selectedCompanyId])

  useEffect(() => {
    setCompanyWorkspacePath(selectedCompany?.workspace_path || '')
    setCompanyDeployCommand(selectedCompany?.deploy_command || '')
  }, [selectedCompany?.id, selectedCompany?.workspace_path, selectedCompany?.deploy_command])

  useEffect(() => {
    loadThreads(selectedAgentId).catch((err) => setStatusMessage(`Failed to load threads: ${err.message}`))
    loadAgentInbox(selectedAgentId, selectedCompanyId).catch(() => {})
  }, [selectedAgentId, selectedCompanyId])

  useEffect(() => {
    loadMessages(selectedThreadId).catch((err) => setStatusMessage(`Failed to load messages: ${err.message}`))
  }, [selectedThreadId])

  useEffect(() => {
    if (activeTab !== 'governance') return
    loadGovernance().catch((err) => setStatusMessage(`Failed to load governance data: ${err.message}`))
    // selected inputs control refresh; helper is recreated on render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeTab, selectedCompanyId])

  useEffect(() => {
    if (!selectedThreadId) return
    const timer = setInterval(() => {
      loadMessages(selectedThreadId).catch(() => {})
    }, 1800)
    return () => clearInterval(timer)
  }, [selectedThreadId])

  useEffect(() => {
    if (!selectedCompanyId) return

    const source = new EventSource(`/api/events/stream?company_id=${encodeURIComponent(selectedCompanyId)}`)

    source.addEventListener('event', (event) => {
      try {
        const payload = JSON.parse(event.data)
        setEvents((previous) => [...previous, payload].slice(-240))
        setLastEventId((previous) => Math.max(previous, payload?.id || 0))

        const eventType = String(payload?.event_type || '').toLowerCase()
        if (eventType.includes('company_') || eventType.includes('department_') || eventType.includes('agent_')) {
          loadCompanies().catch(() => {})
          loadDirectoryData().catch(() => {})
        }
        if (payload?.task_id) {
          api('/api/tasks?limit=300').then((items) => setTasks(items || [])).catch(() => {})
        }
        if (payload?.thread_id) {
          loadMessages(payload.thread_id).catch(() => {})
        }
      } catch {
        // ignore parse errors
      }
    })

    source.addEventListener('error', () => {
      source.close()
      setTimeout(() => {
        api(`/api/events?company_id=${encodeURIComponent(selectedCompanyId)}&since_id=${lastEventId}&limit=100`)
          .then((items) => {
            if (!Array.isArray(items) || items.length === 0) return
            setEvents((previous) => [...previous, ...items].slice(-240))
            const maxID = items.reduce((maxValue, item) => Math.max(maxValue, item?.id || 0), 0)
            setLastEventId((previous) => Math.max(previous, maxID))
          })
          .catch(() => {})
      }, 2000)
    })

    return () => source.close()
  }, [selectedCompanyId, lastEventId])

  useEffect(() => {
    if (modalType === 'agent' && departmentsInModalCompany.length > 0) {
      if (!departmentsInModalCompany.some((department) => department.id === modalDepartmentId)) {
        setModalDepartmentId(departmentsInModalCompany[0].id)
      }
    }
  }, [modalType, departmentsInModalCompany, modalDepartmentId])

  const closeModal = () => {
    setModalType('')
    setModalCompanyId('')
    setModalDepartmentId('')
  }

  const openCompanyModal = () => {
    setNewCompanyName('')
    setNewCompanyPath('')
    setNewCompanyDeployCommand('')
    setModalType('company')
  }

  const openDepartmentModal = (companyID = '') => {
    if (companies.length === 0) return
    const targetCompanyID = companyID || selectedCompanyId || companies[0]?.id || ''
    if (!targetCompanyID) return
    setNewDepartmentName('')
    setModalCompanyId(targetCompanyID)
    setModalType('department')
  }

  const openAgentModal = (departmentID = '') => {
    if (departments.length === 0) return
    const targetDepartmentID = departmentID || selectedDepartmentId || departments[0]?.id || ''
    const targetDepartment = departments.find((department) => department.id === targetDepartmentID)
    if (!targetDepartment) return
    setNewAgentName('')
    setNewAgentRole('worker')
    setModalCompanyId(targetDepartment.company_id)
    setModalDepartmentId(targetDepartmentID)
    setModalType('agent')
  }

  const createCompany = async () => {
    const name = newCompanyName.trim()
    if (!name) return
    await api('/api/companies', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        name,
        description: 'Agent OS company',
        workspace_path: newCompanyPath.trim(),
        deploy_command: newCompanyDeployCommand.trim(),
      }),
    })
    await loadCompanies()
    await loadDirectoryData()
    closeModal()
  }

  const createDepartment = async () => {
    const companyID = modalCompanyId || selectedCompanyId
    const name = newDepartmentName.trim()
    if (!companyID || !name) return
    await api('/api/departments', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ company_id: companyID, name, type: 'general' }),
    })
    await loadDirectoryData()
    if (selectedCompanyId === companyID) {
      await loadCompanyRuntimeData(companyID)
    }
    closeModal()
  }

  const createAgent = async () => {
    const companyID = modalCompanyId || selectedCompanyId
    const departmentID = modalDepartmentId || selectedDepartmentId
    const name = newAgentName.trim()
    if (!companyID || !departmentID || !name) return
    await api('/api/agents', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        company_id: companyID,
        department_id: departmentID,
        name,
        role_type: newAgentRole,
        identity_prompt: `You are ${name}, operating as part of Agent OS.`,
      }),
    })
    await loadDirectoryData()
    closeModal()
  }

  const updateCompanyWorkspace = async () => {
    if (!selectedCompanyId) return
    await api(`/api/companies/${encodeURIComponent(selectedCompanyId)}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        workspace_path: companyWorkspacePath.trim(),
        deploy_command: companyDeployCommand.trim(),
      }),
    })
    await loadCompanies()
    setStatusMessage('Company workspace mapping updated.')
  }

  const bindAgentModel = async () => {
    if (!selectedAgentId || !selectedProfileId) return
    await api(`/api/agents/${encodeURIComponent(selectedAgentId)}/model-bind`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        primary_profile_id: selectedProfileId,
        temperature: 0.2,
        max_tokens: 1400,
        reasoning_effort: 'standard',
      }),
    })
    setStatusMessage('Model profile was bound to selected agent.')
  }

  const createThread = async () => {
    if (!selectedCompanyId || !selectedAgentId) return
    const created = await api('/api/threads', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        company_id: selectedCompanyId,
        department_id: selectedDepartmentId,
        agent_id: selectedAgentId,
        title: newThreadTitle.trim() || 'New Agent Thread',
      }),
    })
    setNewThreadTitle('')
    await loadThreads(selectedAgentId)
    if (created?.id) {
      setSelectedThreadId(created.id)
      await loadMessages(created.id)
    }
  }

  const sendChatMessage = async () => {
    const value = chatInput.trim()
    if (!value || !selectedAgentId || !selectedCompanyId) return

    let targetThreadID = selectedThreadId
    if (!targetThreadID) {
      const created = await api('/api/threads', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          company_id: selectedCompanyId,
          department_id: selectedDepartmentId,
          agent_id: selectedAgentId,
          title: 'Autocreated Thread',
        }),
      })
      targetThreadID = created.id
      setSelectedThreadId(created.id)
      await loadThreads(selectedAgentId)
    }

    setChatInput('')
    await api(`/api/threads/${encodeURIComponent(targetThreadID)}/messages`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ role: 'user', content: value }),
    })
    await loadMessages(targetThreadID)
  }

  const createSchedule = async () => {
    if (!selectedCompanyId || !selectedAgentId) return
    const payload = { prompt: scheduleMessage || 'Scheduled task' }
    if (scheduleMode === 'cron') {
      if (!scheduleExpr.trim()) return
      await api('/api/schedules', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          company_id: selectedCompanyId,
          department_id: selectedDepartmentId || '',
          target_agent_id: selectedAgentId,
          schedule_type: 'cron',
          cron_expr: scheduleExpr.trim(),
          timezone: 'UTC',
          payload_json: JSON.stringify(payload),
          is_active: true,
        }),
      })
    } else {
      if (!scheduleOnce) return
      const dt = new Date(scheduleOnce).toISOString()
      await api('/api/schedules', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          company_id: selectedCompanyId,
          department_id: selectedDepartmentId || '',
          target_agent_id: selectedAgentId,
          schedule_type: 'once',
          start_at: dt,
          timezone: 'UTC',
          payload_json: JSON.stringify(payload),
          is_active: true,
        }),
      })
    }
    await loadCompanyRuntimeData(selectedCompanyId)
  }

  const sendInterAgentMessage = async () => {
    if (!interAgentTo || !interAgentContent.trim() || !selectedAgentId || !selectedCompanyId) return
    await api('/api/inter-agent', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        from_agent_id: selectedAgentId,
        to_agent_id: interAgentTo,
        content: interAgentContent,
        company_id: selectedCompanyId,
      }),
    })
    setInterAgentContent('')
    await loadAgentInbox(selectedAgentId, selectedCompanyId)
  }

  const queryMemory = async () => {
    if (!selectedCompanyId) return
    const items = await api(`/api/memory/query?company_id=${encodeURIComponent(selectedCompanyId)}&department_id=${encodeURIComponent(selectedDepartmentId || '')}&agent_id=${encodeURIComponent(selectedAgentId || '')}&query=${encodeURIComponent(memoryQuery)}&limit=40`)
    setMemoryHits(items || [])
  }

  const resolveApproval = async (approvalID, decision) => {
    await api('/api/approvals/resolve', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ approval_id: approvalID, decision, actor: 'owner' }),
    })
    await loadGovernance()
  }

  const onboardingSteps = [
    { label: 'Company', done: companies.length > 0, action: openCompanyModal },
    { label: 'Department', done: departments.length > 0, action: () => openDepartmentModal() },
    { label: 'Agent', done: agents.length > 0, action: () => openAgentModal() },
    { label: 'Model', done: modelProfiles.length > 0, action: null },
  ]

  const renderOrganizationTree = () => {
    if (companies.length === 0) {
      return (
        <div className="agentos-tree-empty-state">
          <button className="btn-primary" type="button" onClick={openCompanyModal}>Create Company</button>
        </div>
      )
    }

    return (
      <>
        <div className="agentos-section-header">
          <h3>Organization</h3>
          <button className="btn-secondary" type="button" onClick={openCompanyModal}>+ Company</button>
        </div>

        <div className="agentos-tree">
          {companies.map((company) => {
            const isCompanyActive = selectedCompanyId === company.id
            const scopedDepartments = departmentsByCompany[company.id] || []
            return (
              <div key={company.id} className="tree-company-block">
                <button
                  className={`tree-node tree-company ${isCompanyActive ? 'active' : ''}`}
                  type="button"
                  onClick={() => setSelectedCompanyId(company.id)}
                >
                  {company.name}
                </button>

                {isCompanyActive && (
                  <div className="tree-children">
                    {scopedDepartments.length === 0 && (
                      <div className="tree-empty-with-action">
                        <div className="tree-empty">No departments</div>
                        <button className="btn-secondary" type="button" onClick={() => openDepartmentModal(company.id)}>
                          Create Department
                        </button>
                      </div>
                    )}

                    {scopedDepartments.map((department) => {
                      const scopedAgents = agentsByDepartment[department.id] || []
                      const isDepartmentActive = selectedDepartmentId === department.id
                      return (
                        <div key={department.id} className="tree-department-block">
                          <button
                            className={`tree-node tree-department ${isDepartmentActive ? 'active' : ''}`}
                            type="button"
                            onClick={() => setSelectedDepartmentId(department.id)}
                          >
                            {department.name}
                          </button>

                          <div className="tree-children">
                            {scopedAgents.length === 0 && (
                              <div className="tree-empty-with-action">
                                <div className="tree-empty">No agents</div>
                                <button className="btn-secondary" type="button" onClick={() => openAgentModal(department.id)}>
                                  Create Agent
                                </button>
                              </div>
                            )}

                            {scopedAgents.map((agent) => (
                              <button
                                key={agent.id}
                                className={`tree-node tree-agent ${selectedAgentId === agent.id ? 'active' : ''}`}
                                type="button"
                                onClick={() => {
                                  setSelectedCompanyId(company.id)
                                  setSelectedDepartmentId(department.id)
                                  setSelectedAgentId(agent.id)
                                  setActiveTab('chat')
                                }}
                              >
                                <span className="tree-agent-name">{agent.name}</span>
                                <span className="tree-agent-role">{agent.role_type}</span>
                              </button>
                            ))}
                          </div>
                        </div>
                      )
                    })}
                  </div>
                )}
              </div>
            )
          })}
        </div>
      </>
    )
  }

  return (
    <div className="agentos-shell">
      <aside className="agentos-sidebar">
        <div className="agentos-section agentos-tree-section">
          {renderOrganizationTree()}
        </div>

        {selectedCompanyId && (
          <div className="agentos-section">
            <h3>Company Folder</h3>
            <div className="agentos-row">
              <input
                type="text"
                value={companyWorkspacePath}
                onChange={(event) => setCompanyWorkspacePath(event.target.value)}
                placeholder="Assigned folder path"
              />
            </div>
            <div className="agentos-row">
              <input
                type="text"
                value={companyDeployCommand}
                onChange={(event) => setCompanyDeployCommand(event.target.value)}
                placeholder="Deploy command"
              />
              <button
                className="btn-secondary"
                type="button"
                onClick={() => updateCompanyWorkspace().catch((err) => setStatusMessage(err.message))}
              >
                Save
              </button>
            </div>
          </div>
        )}
      </aside>

      <main className="agentos-main">
        <header className="agentos-mainbar">
          <div className="agentos-mainbar-title">
            <h2>{selectedCompany?.name || 'No Company Selected'}</h2>
            <span>
              {selectedAgent
                ? `${selectedAgent.name} · ${selectedAgent.role_type}`
                : 'Select an agent in the left tree to open chat'}
            </span>
          </div>
        </header>

        {statusMessage && <div className="agentos-status">{statusMessage}</div>}

        {activeTab === 'companies' && (
          <section className="agentos-panel">
            <div className="agentos-panel-head">
              <div>
                <h3>AgentHQ Command Center</h3>
                <p className="agentos-panel-sub">Operate companies, agents, schedules, memory, and governance from one workspace.</p>
              </div>
              <button className="btn-primary" type="button" onClick={openCompanyModal}>Add Company</button>
            </div>

            <div className="ahq-overview-grid">
              <div className="ahq-overview-card ahq-activity-card">
                <div className="ahq-card-title-row">
                  <div>
                    <h4>Today at a glance</h4>
                    <span>{selectedCompany?.name || 'No company selected'}</span>
                  </div>
                  <Activity size={18} />
                </div>
                <div className="ahq-blob-viz" aria-hidden="true">
                  <span className="blob blob-yellow"><strong>{activeSchedules.length}</strong><small>active runs</small></span>
                  <span className="blob blob-coral"><strong>{runningTasks.length}</strong><small>running</small></span>
                  <span className="blob blob-dark"><strong>{agents.length}</strong><small>agents</small></span>
                </div>
                <div className="ahq-viz-legend">
                  <span><i className="legend-yellow" />Schedules</span>
                  <span><i className="legend-coral" />Tasks</span>
                  <span><i className="legend-dark" />Agents</span>
                </div>
              </div>

              <div className="ahq-overview-card ahq-progress-card">
                <div className="ahq-card-title-row">
                  <div>
                    <h4>Setup path</h4>
                    <span>First useful agent company</span>
                  </div>
                  <CheckCircle2 size={18} />
                </div>
                <div className="ahq-step-dots">
                  {onboardingSteps.map((step) => (
                    <button
                      key={step.label}
                      type="button"
                      className={step.done ? 'done' : ''}
                      onClick={step.action || (() => setActiveTab('agents'))}
                      title={step.label}
                    />
                  ))}
                </div>
                <div className="ahq-step-list">
                  {onboardingSteps.map((step) => (
                    <button key={step.label} type="button" onClick={step.action || (() => setActiveTab('agents'))}>
                      <span className={step.done ? 'done' : ''}>{step.done ? 'Done' : 'Next'}</span>
                      {step.label}
                    </button>
                  ))}
                </div>
              </div>
            </div>

            <div className="agentos-kpi-row ahq-metric-row">
              <OverviewMetric icon={Bot} label="Agents" value={agents.length} tone="yellow" />
              <OverviewMetric icon={CalendarDays} label="Active schedules" value={activeSchedules.length} tone="coral" />
              <OverviewMetric icon={Brain} label="Memory entries" value={memoryTimeline.length} tone="green" />
              <OverviewMetric icon={ShieldCheck} label="Open approvals" value={approvals.length} tone="dark" />
            </div>

            {companies.length === 0 && (
              <EmptyAction
                title="Create your first AI company"
                body="Start with a company, then add one department and one manager agent."
                action="Create Company"
                onClick={openCompanyModal}
              />
            )}

            <div className="agentos-table-wrap">
              <table className="agentos-table">
                <thead>
                  <tr>
                    <th>Name</th>
                    <th>Departments</th>
                    <th>Agents</th>
                    <th>Running Tasks</th>
                    <th>Workspace</th>
                  </tr>
                </thead>
                <tbody>
                  {companies.length === 0 && (
                    <tr>
                      <td colSpan={5} className="agentos-empty-cell">No companies yet.</td>
                    </tr>
                  )}
                  {companies.map((company) => {
                    const companyTasks = tasksByCompany[company.id] || []
                    return (
                      <tr key={company.id}>
                        <td>
                          <button
                            className="agentos-link-btn"
                            type="button"
                            onClick={() => {
                              setSelectedCompanyId(company.id)
                              setActiveTab('departments')
                            }}
                          >
                            {company.name}
                          </button>
                        </td>
                        <td>{(departmentsByCompany[company.id] || []).length}</td>
                        <td>{(agentsByCompany[company.id] || []).length}</td>
                        <td>{companyTasks.filter((task) => task.status === 'running').length}</td>
                        <td>{company.workspace_path || '-'}</td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          </section>
        )}

        {activeTab === 'departments' && (
          <section className="agentos-panel">
            <div className="agentos-panel-head">
              <h3>Departments</h3>
              <button
                className="btn-primary"
                type="button"
                disabled={companies.length === 0}
                onClick={() => openDepartmentModal()}
              >
                Add Department
              </button>
            </div>
            {companies.length === 0 && <div className="agentos-empty">Create a company first.</div>}

            <div className="agentos-table-wrap">
              <table className="agentos-table">
                <thead>
                  <tr>
                    <th>Department</th>
                    <th>Company</th>
                    <th>Agents</th>
                    <th>Managers</th>
                    <th>Running Tasks</th>
                    <th>Total Tasks</th>
                  </tr>
                </thead>
                <tbody>
                  {departments.length === 0 && (
                    <tr>
                      <td colSpan={6} className="agentos-empty-cell">No departments yet.</td>
                    </tr>
                  )}
                  {departments.map((department) => {
                    const departmentAgentsList = agentsByDepartment[department.id] || []
                    const departmentTasks = tasksByDepartment[department.id] || []
                    return (
                      <tr key={department.id}>
                        <td>
                          <button
                            className="agentos-link-btn"
                            type="button"
                            onClick={() => {
                              setSelectedCompanyId(department.company_id)
                              setSelectedDepartmentId(department.id)
                            }}
                          >
                            {department.name}
                          </button>
                        </td>
                        <td>{companyByID[department.company_id]?.name || '-'}</td>
                        <td>{departmentAgentsList.length}</td>
                        <td>{departmentAgentsList.filter((agent) => agent.role_type === 'manager').length}</td>
                        <td>{departmentTasks.filter((task) => task.status === 'running').length}</td>
                        <td>{departmentTasks.length}</td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          </section>
        )}

        {activeTab === 'agents' && (
          <section className="agentos-panel">
            <div className="agentos-panel-head">
              <h3>Agents</h3>
              <div className="agentos-row compact wrap">
                <select value={selectedProfileId} onChange={(event) => setSelectedProfileId(event.target.value)}>
                  <option value="">Model profile</option>
                  {modelProfiles.map((profile) => (
                    <option key={profile.id} value={profile.id}>{profile.provider}: {profile.model}</option>
                  ))}
                </select>
                <button className="btn-secondary" type="button" onClick={() => bindAgentModel().catch((err) => setStatusMessage(err.message))}>
                  Bind to Selected
                </button>
                <button
                  className="btn-primary"
                  type="button"
                  disabled={departments.length === 0}
                  onClick={() => openAgentModal()}
                >
                  Add Agent
                </button>
              </div>
            </div>
            {departments.length === 0 && (
              <EmptyAction
                title="Agents need a department"
                body="Create a department first so each worker has a clear reporting line."
                action="Create Department"
                onClick={() => openDepartmentModal()}
                disabled={companies.length === 0}
              />
            )}

            <div className="agentos-table-wrap">
              <table className="agentos-table">
                <thead>
                  <tr>
                    <th>Agent</th>
                    <th>Role</th>
                    <th>Department</th>
                    <th>Company</th>
                    <th>Status</th>
                    <th>Tasks</th>
                    <th>Select</th>
                  </tr>
                </thead>
                <tbody>
                  {agents.length === 0 && (
                    <tr>
                      <td colSpan={7} className="agentos-empty-cell">No agents yet. Add a manager first, then workers.</td>
                    </tr>
                  )}
                  {agents.map((agent) => {
                    const agentTasks = tasks.filter((task) => task.agent_id === agent.id)
                    return (
                      <tr key={agent.id}>
                        <td>{agent.name}</td>
                        <td>{agent.role_type}</td>
                        <td>{departmentByID[agent.department_id]?.name || '-'}</td>
                        <td>{companyByID[agent.company_id]?.name || '-'}</td>
                        <td>
                          <span className="agentos-status-badge" data-status={agent.status || 'idle'}>
                            {agent.status || 'idle'}
                          </span>
                        </td>
                        <td>{agentTasks.length}</td>
                        <td>
                          <button
                            className={`btn-secondary ${selectedAgentId === agent.id ? 'is-active' : ''}`}
                            type="button"
                            onClick={() => {
                              setSelectedCompanyId(agent.company_id)
                              setSelectedDepartmentId(agent.department_id)
                              setSelectedAgentId(agent.id)
                              setActiveTab('chat')
                            }}
                          >
                            Open
                          </button>
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>

            {selectedAgentId && (
              <div className="agentos-section" style={{marginTop:'10px'}}>
                <h3>Inter-agent Messages</h3>
                <div className="agentos-row" style={{marginBottom:'8px',gap:'6px'}}>
                  <select
                    value={interAgentTo}
                    onChange={(e) => setInterAgentTo(e.target.value)}
                    style={{flex:1}}
                  >
                    <option value="">Send to agent...</option>
                    {agents.filter(a => a.id !== selectedAgentId).map(a => (
                      <option key={a.id} value={a.id}>{a.name}</option>
                    ))}
                  </select>
                </div>
                <div className="agentos-row" style={{gap:'6px'}}>
                  <textarea
                    value={interAgentContent}
                    onChange={(e) => setInterAgentContent(e.target.value)}
                    placeholder="Message content..."
                    style={{flex:1,minHeight:'52px',maxHeight:'100px',resize:'vertical'}}
                  />
                  <button className="btn-primary" type="button" onClick={() => sendInterAgentMessage().catch(e => setStatusMessage(e.message))}>Send</button>
                </div>
                <div className="agentos-scroll small" style={{marginTop:'8px'}}>
                  {interAgentInbox.length === 0 && <div className="agentos-empty">No inter-agent messages yet.</div>}
                  {interAgentInbox.map((msg, i) => (
                    <div key={i} className="agentos-message" style={{marginBottom:'4px'}}>
                      <div className="agentos-message-role">{msg.role?.replace('agent:', '') || 'system'} — {msg.created_at ? new Date(msg.created_at).toLocaleString() : ''}</div>
                      <div className="agentos-message-content">{msg.content}</div>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </section>
        )}

        {activeTab === 'chat' && (
          <section className="agentos-panel">
            <div className="agentos-row chat-top-row">
              <input
                type="text"
                value={newThreadTitle}
                onChange={(event) => setNewThreadTitle(event.target.value)}
                placeholder="New thread title"
              />
              <button className="btn-secondary" type="button" onClick={createThread}>Create Thread</button>
              <select value={selectedThreadId} onChange={(event) => setSelectedThreadId(event.target.value)}>
                <option value="">Select thread</option>
                {threads.map((thread) => (
                  <option key={thread.id} value={thread.id}>{thread.title}</option>
                ))}
              </select>
            </div>

            <div className="agentos-chat">
              {messages.map((message) => (
                <div key={message.id} className={`agentos-message ${message.role}`}>
                  <div className="agentos-message-role">{message.role}</div>
                  <div className="agentos-message-content">{message.content}</div>
                </div>
              ))}
              {messages.length === 0 && <div className="agentos-empty">Select an agent and start chatting.</div>}
            </div>

            <div className="agentos-row">
              <textarea
                value={chatInput}
                onChange={(event) => setChatInput(event.target.value)}
                placeholder={selectedAgent ? `Message ${selectedAgent.name}` : 'Select an agent first'}
                onKeyDown={(event) => {
                  if (event.key === 'Enter' && !event.shiftKey) {
                    event.preventDefault()
                    sendChatMessage().catch((err) => setStatusMessage(err.message))
                  }
                }}
              />
              <button className="btn-primary" type="button" onClick={() => sendChatMessage().catch((err) => setStatusMessage(err.message))}>Send</button>
            </div>
          </section>
        )}

        {activeTab === 'tasks' && (
          <section className="agentos-panel">
            <div className="agentos-table-wrap">
              <table className="agentos-table">
                <thead>
                  <tr>
                    <th>Task Type</th>
                    <th>Status</th>
                    <th>Agent</th>
                    <th>Updated</th>
                    <th>Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {tasks.length === 0 && (
                    <tr>
                      <td colSpan={5} className="agentos-empty-cell">No tasks yet.</td>
                    </tr>
                  )}
                  {tasks.map((task) => (
                    <tr key={task.id}>
                      <td>{task.type || 'task'}</td>
                      <td>
                        <span className="agentos-status-badge" data-status={task.status || 'queued'}>
                          {task.status || 'queued'}
                        </span>
                      </td>
                      <td>{agentByID[task.agent_id]?.name || task.agent_id || '-'}</td>
                      <td>{task.updated_at || '-'}</td>
                      <td>
                        <div className="agentos-row compact">
                          <button className="btn-secondary" type="button" onClick={() => api(`/api/tasks/${task.id}/retry`, { method: 'POST' }).then(loadDirectoryData).catch((err) => setStatusMessage(err.message))}>Retry</button>
                          <button className="btn-secondary" type="button" onClick={() => api(`/api/tasks/${task.id}/cancel`, { method: 'POST' }).then(loadDirectoryData).catch((err) => setStatusMessage(err.message))}>Cancel</button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </section>
        )}

        {activeTab === 'calendar' && (
          <section className="agentos-panel">
            {!selectedAgentId && (
              <EmptyAction
                title="Select an agent to schedule work"
                body="Schedules target a single agent. Choose one from the organization tree or Agents tab."
                action="Open Agents"
                onClick={() => setActiveTab('agents')}
              />
            )}
            <div className="agentos-row" style={{gap: '8px', flexWrap: 'wrap'}}>
              <select
                value={scheduleMode}
                onChange={(e) => setScheduleMode(e.target.value)}
              >
                <option value="cron">Cron (recurring)</option>
                <option value="once">One-time (datetime)</option>
              </select>
              {scheduleMode === 'cron' ? (
                <input
                  type="text"
                  value={scheduleExpr}
                  onChange={(e) => setScheduleExpr(e.target.value)}
                  placeholder="Cron expression (e.g. 0 9 * * MON)"
                  style={{flex:1,minWidth:'160px'}}
                />
              ) : (
                <input
                  type="datetime-local"
                  value={scheduleOnce}
                  onChange={(e) => setScheduleOnce(e.target.value)}
                  style={{flex:1,minWidth:'200px'}}
                />
              )}
              <input
                type="text"
                value={scheduleMessage}
                onChange={(e) => setScheduleMessage(e.target.value)}
                placeholder="Task prompt for agent"
                style={{flex:2,minWidth:'200px'}}
              />
              <button className="btn-primary" type="button" onClick={() => createSchedule().catch((err) => setStatusMessage(err.message))}>Add Schedule</button>
            </div>

            <div className="agentos-kpi-row">
              <div className="agentos-kpi-card"><span>Schedules</span><strong>{schedules.length}</strong></div>
              <div className="agentos-kpi-card"><span>Active</span><strong>{schedules.filter((schedule) => schedule.is_active).length}</strong></div>
              <div className="agentos-kpi-card"><span>Paused</span><strong>{schedules.filter((schedule) => !schedule.is_active).length}</strong></div>
            </div>

            <div className="agentos-two-col">
              <div className="agentos-scroll">
                <h4>Upcoming Runs</h4>
                {schedules.length === 0 && <div className="agentos-empty">No schedules yet. Add a recurring cron or one-time run above.</div>}
                {schedules.map((schedule) => (
                  <div key={schedule.id} className="agentos-list-item">
                    <div className="agentos-list-title">{schedule.schedule_type} • {schedule.is_active ? 'active' : 'paused'}</div>
                    <div className="agentos-list-meta">next: {schedule.next_run_at || 'n/a'} • expr: {schedule.cron_expr || schedule.rrule || '-'}</div>
                    <button
                      className="btn-secondary"
                      type="button"
                      onClick={() => api(`/api/schedules/${schedule.id}/toggle`, {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ enabled: !schedule.is_active }),
                      }).then(() => loadCompanyRuntimeData(selectedCompanyId)).catch((err) => setStatusMessage(err.message))}
                    >
                      {schedule.is_active ? 'Pause' : 'Resume'}
                    </button>
                  </div>
                ))}
              </div>
              <div className="agentos-scroll">
                <h4>Schedule Events</h4>
                {scheduleEvents.length === 0 && <div className="agentos-empty">No schedule events yet.</div>}
                {scheduleEvents.map((event) => (
                  <div key={`${event.id}-${event.event_type}`} className="agentos-list-item">
                    <div className="agentos-list-title">{event.event_type}</div>
                    <div className="agentos-list-meta">severity: {event.severity || 'info'} • {event.created_at || '-'}</div>
                  </div>
                ))}
              </div>
            </div>
          </section>
        )}

        {activeTab === 'memory' && (
          <section className="agentos-panel">
            <div className="agentos-row">
              <input type="text" value={memoryQuery} onChange={(event) => setMemoryQuery(event.target.value)} placeholder="Search memory" />
              <button className="btn-primary" type="button" onClick={() => queryMemory().catch((err) => setStatusMessage(err.message))}>Query</button>
            </div>

            <div className="agentos-kpi-row">
              <div className="agentos-kpi-card"><span>Timeline Entries</span><strong>{memoryTimeline.length}</strong></div>
              <div className="agentos-kpi-card"><span>Query Results</span><strong>{memoryHits.length}</strong></div>
              <div className="agentos-kpi-card"><span>Company Events</span><strong>{events.length}</strong></div>
            </div>

            <div className="agentos-two-col">
              <div className="agentos-scroll">
                <h4>Memory Timeline</h4>
                {memoryTimeline.map((entry) => (
                  <div key={entry.id} className="agentos-list-item">
                    <div className="agentos-list-title">{entry.scope_type} • {entry.source_type}</div>
                    <div className="agentos-list-meta">{entry.content}</div>
                  </div>
                ))}
              </div>
              <div className="agentos-scroll">
                <h4>Search Results</h4>
                {memoryHits.length === 0 && <div className="agentos-empty">Run a query to see results.</div>}
                {memoryHits.map((entry) => (
                  <div key={entry.id} className="agentos-list-item">
                    <div className="agentos-list-title">{entry.scope_type} • {entry.source_type}</div>
                    <div className="agentos-list-meta">{entry.content}</div>
                  </div>
                ))}
              </div>
            </div>
          </section>
        )}

        {activeTab === 'governance' && (
          <section className="agentos-panel">
            <div className="agentos-kpi-row">
              <div className="agentos-kpi-card"><span>Policies</span><strong>{policies.length}</strong></div>
              <div className="agentos-kpi-card"><span>Pending Approvals</span><strong>{approvals.length}</strong></div>
              <div className="agentos-kpi-card"><span>Audit Health</span><strong>{auditVerify?.ok ? 'OK' : 'Issue'}</strong></div>
            </div>

            <div className="agentos-two-col">
              <div className="agentos-scroll">
                <h4>Policies Matrix</h4>
                <div className="agentos-table-wrap">
                  <table className="agentos-table">
                    <thead>
                      <tr>
                        <th>Effect</th>
                        <th>Action</th>
                        <th>Scope</th>
                        <th>Tier</th>
                      </tr>
                    </thead>
                    <tbody>
                      {policies.length === 0 && (
                        <tr>
                          <td colSpan={4} className="agentos-empty-cell">No policies configured.</td>
                        </tr>
                      )}
                      {policies.map((policy) => (
                        <tr key={policy.id}>
                          <td>{policy.effect}</td>
                          <td>{policy.action}</td>
                          <td>{policy.scope_pattern || '*'}</td>
                          <td>{policy.approval_tier || 'none'}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>

              <div className="agentos-scroll">
                <h4>Approvals Queue</h4>
                {approvals.length === 0 && <div className="agentos-empty">No pending approvals.</div>}
                {approvals.map((approval) => (
                  <div key={approval.id} className="agentos-list-item">
                    <div className="agentos-list-title">{approval.action} • {approval.tier}</div>
                    <div className="agentos-list-meta">{approval.reason}</div>
                    <div className="agentos-row compact">
                      <button className="btn-primary" type="button" onClick={() => resolveApproval(approval.id, 'approve').catch((err) => setStatusMessage(err.message))}>Approve</button>
                      <button className="btn-secondary" type="button" onClick={() => resolveApproval(approval.id, 'reject').catch((err) => setStatusMessage(err.message))}>Reject</button>
                    </div>
                  </div>
                ))}

                <h4>Audit Verify</h4>
                <div className="agentos-list-item">
                  <div className="agentos-list-title">{auditVerify?.ok ? 'Chain Verified' : 'Issues Found'}</div>
                  <div className="agentos-list-meta">entries: {auditVerify?.entries || 0}</div>
                  {!auditVerify?.ok && Array.isArray(auditVerify?.issues) && (
                    <div className="agentos-list-meta">{auditVerify.issues.join('; ')}</div>
                  )}
                </div>
              </div>
            </div>
          </section>
        )}

        <footer className="agentos-events">
          <h4>Company Event Stream</h4>
          <div className="agentos-scroll small">
            {events.slice(-24).map((event) => (
              <div key={`${event.id}-${event.event_type}`} className="agentos-event-line">
                <span>[{event.event_type}]</span> <span>{event.severity}</span> <span>{event.created_at}</span>
              </div>
            ))}
          </div>
        </footer>
      </main>

      {modalType && (
        <div className="modal-overlay" onClick={closeModal}>
          <div className="modal-content agentos-modal" onClick={(event) => event.stopPropagation()}>
            {modalType === 'company' && (
              <>
                <div className="card-title">Create Company</div>
                <div className="agentos-modal-body">
                  <input
                    type="text"
                    value={newCompanyName}
                    onChange={(event) => setNewCompanyName(event.target.value)}
                    placeholder="Company name"
                  />
                  <input
                    type="text"
                    value={newCompanyPath}
                    onChange={(event) => setNewCompanyPath(event.target.value)}
                    placeholder="Folder path"
                  />
                  <input
                    type="text"
                    value={newCompanyDeployCommand}
                    onChange={(event) => setNewCompanyDeployCommand(event.target.value)}
                    placeholder="Deploy command"
                  />
                </div>
                <div className="agentos-modal-actions">
                  <button className="btn-secondary" type="button" onClick={closeModal}>Cancel</button>
                  <button className="btn-primary" type="button" onClick={() => createCompany().catch((err) => setStatusMessage(err.message))}>Create</button>
                </div>
              </>
            )}

            {modalType === 'department' && (
              <>
                <div className="card-title">Create Department</div>
                <div className="agentos-modal-body">
                  <select value={modalCompanyId} onChange={(event) => setModalCompanyId(event.target.value)}>
                    {companies.map((company) => (
                      <option key={company.id} value={company.id}>{company.name}</option>
                    ))}
                  </select>
                  <input
                    type="text"
                    value={newDepartmentName}
                    onChange={(event) => setNewDepartmentName(event.target.value)}
                    placeholder="Department name"
                  />
                </div>
                <div className="agentos-modal-actions">
                  <button className="btn-secondary" type="button" onClick={closeModal}>Cancel</button>
                  <button className="btn-primary" type="button" onClick={() => createDepartment().catch((err) => setStatusMessage(err.message))}>Create</button>
                </div>
              </>
            )}

            {modalType === 'agent' && (
              <>
                <div className="card-title">Create Agent</div>
                <div className="agentos-modal-body">
                  <select value={modalCompanyId} onChange={(event) => setModalCompanyId(event.target.value)}>
                    {companies.map((company) => (
                      <option key={company.id} value={company.id}>{company.name}</option>
                    ))}
                  </select>
                  <select value={modalDepartmentId} onChange={(event) => setModalDepartmentId(event.target.value)}>
                    {departmentsInModalCompany.map((department) => (
                      <option key={department.id} value={department.id}>{department.name}</option>
                    ))}
                  </select>
                  <input
                    type="text"
                    value={newAgentName}
                    onChange={(event) => setNewAgentName(event.target.value)}
                    placeholder="Agent name"
                  />
                  <select value={newAgentRole} onChange={(event) => setNewAgentRole(event.target.value)}>
                    <option value="worker">Worker</option>
                    <option value="manager">Manager</option>
                  </select>
                </div>
                <div className="agentos-modal-actions">
                  <button className="btn-secondary" type="button" onClick={closeModal}>Cancel</button>
                  <button className="btn-primary" type="button" onClick={() => createAgent().catch((err) => setStatusMessage(err.message))}>Create</button>
                </div>
              </>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
