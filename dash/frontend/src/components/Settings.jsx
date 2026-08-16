import { useState, useEffect } from 'react';

function authFetch(url, options = {}) {
  const token = localStorage.getItem('agenthq_token')
  return fetch(url, {
    ...options,
    headers: {
      ...(options.headers || {}),
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
  })
}

function getSessionToken() {
  return localStorage.getItem('agenthq_token') || '';
}

// ─── MCP Server Management Panel ─────────────────────────────────────────────
function MCPPanel() {
  const [servers, setServers] = useState([]);
  const [tools, setTools] = useState([]);
  const [newServer, setNewServer] = useState({ name: '', command: '', transport: 'stdio' });
  const [adding, setAdding] = useState(false);
  const [loading, setLoading] = useState(false);

  const fetchMCP = async () => {
    try {
      const res = await authFetch('/api/mcp');
      const data = await res.json();
      setServers(data.servers || []);
      setTools(data.tools || []);
    } catch {
      // MCP is optional; keep the settings panel usable when it is unavailable.
    }
  };

  useEffect(() => { fetchMCP(); }, []);

  const addServer = async () => {
    if (!newServer.name || !newServer.command) return;
    setLoading(true);
    try {
      await authFetch('/api/mcp', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(newServer)
      });
      setNewServer({ name: '', command: '', transport: 'stdio' });
      setAdding(false);
      setTimeout(fetchMCP, 1500); // wait for server to start + discover tools
    } finally { setLoading(false); }
  };

  const removeServer = async (name) => {
    await authFetch(`/api/mcp?name=${encodeURIComponent(name)}`, { method: 'DELETE' });
    fetchMCP();
  };

  return (
    <section style={{ marginBottom: '32px' }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '12px' }}>
        <h3 className="card-title" style={{ margin: 0 }}>MCP Servers</h3>
        <button onClick={() => setAdding(v => !v)} style={{
          fontSize: '0.78rem', padding: '5px 12px', borderRadius: '8px', cursor: 'pointer',
          background: 'var(--accent-color)', border: 'none', color: '#fff', fontWeight: '600'
        }}>
          {adding ? '✕ Cancel' : '+ Add Server'}
        </button>
      </div>
      <p style={{ color: 'var(--text-secondary)', fontSize: '0.85rem', marginBottom: '14px' }}>
        Connect external tool servers via the Model Context Protocol (stdio transport).
        AgentHQ discovers their tools automatically and makes them available in Coding mode.
      </p>

      {adding && (
        <div style={{ background: 'var(--surface-color)', border: '1px solid var(--border-color)', borderRadius: '8px', padding: '16px', marginBottom: '16px', display: 'flex', flexDirection: 'column', gap: '10px' }}>
          <input placeholder="Server name (e.g. postgres)" value={newServer.name}
            onChange={e => setNewServer(p => ({ ...p, name: e.target.value }))}
            className="settings-input" style={{ width: '100%' }} />
          <select value={newServer.transport} onChange={e => setNewServer(p => ({ ...p, transport: e.target.value }))}
            className="settings-input" style={{ width: '140px' }}>
            <option value="stdio">stdio (Local Command)</option>
            <option value="http">http (Remote URL)</option>
          </select>
          {newServer.transport === 'stdio' ? (
            <input placeholder="Command (e.g. npx @modelcontextprotocol/server-postgres postgresql://...)" value={newServer.command}
              onChange={e => setNewServer(p => ({ ...p, command: e.target.value }))}
              className="settings-input" style={{ width: '100%', fontFamily: 'monospace', fontSize: '0.8rem' }} />
          ) : (
            <input placeholder="Server URL (e.g. http://localhost:3000/rpc)" value={newServer.url}
              onChange={e => setNewServer(p => ({ ...p, url: e.target.value }))}
              className="settings-input" style={{ width: '100%', fontFamily: 'monospace', fontSize: '0.8rem' }} />
          )}
          <button onClick={addServer} disabled={loading || !newServer.name || !newServer.command}
            style={{ alignSelf: 'flex-start', padding: '8px 18px', borderRadius: '8px', fontWeight: '600', fontSize: '0.85rem',
              background: loading ? 'var(--border-color)' : 'var(--accent-color)', border: 'none', color: '#fff', cursor: loading ? 'not-allowed' : 'pointer' }}>
            {loading ? 'Starting…' : 'Connect'}
          </button>
        </div>
      )}

      {servers && servers.length > 0 && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: '10px', marginBottom: '14px' }}>
          {servers.map(s => {
            const serverTools = tools.filter(t => t.server === s.name);
            return (
              <div key={s.name} style={{ background: 'var(--surface-color)', border: '1px solid var(--border-color)', borderRadius: '8px', padding: '12px 14px' }}>
                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                  <div>
                    <span style={{ fontWeight: '700', fontSize: '0.88rem' }}>🔌 {s.name}</span>
                    <span style={{ fontSize: '0.75rem', color: 'var(--text-secondary)', marginLeft: '8px' }}>{s.transport || 'stdio'}</span>
                    
                    <span style={{ 
                      display: 'inline-block', width: '8px', height: '8px', borderRadius: '50%', marginLeft: '12px',
                      background: s.status === 'running' ? '#22c55e' : (s.status === 'crashed' ? '#f87171' : '#f59e0b')
                    }} />
                    <span style={{ fontSize: '0.7rem', color: 'var(--text-secondary)', marginLeft: '5px' }}>
                      {s.status}
                    </span>
                    
                    {serverTools.length > 0 && (
                      <span style={{ fontSize: '0.7rem', color: '#22c55e', marginLeft: '8px', fontWeight: '600' }}>
                        {serverTools.length} tools
                      </span>
                    )}
                  </div>
                  <button onClick={() => removeServer(s.name)} style={{
                    background: 'transparent', border: 'none', cursor: 'pointer', color: '#f87171', fontSize: '1rem', padding: '2px 6px'
                  }}>✕</button>
                </div>
                {serverTools.length > 0 && (
                  <div style={{ marginTop: '8px', display: 'flex', flexWrap: 'wrap', gap: '5px' }}>
                    {serverTools.map(t => (
                      <span key={t.name} title={t.description} style={{
                        fontSize: '0.72rem', background: 'rgba(139,92,246,0.12)', border: '1px solid rgba(139,92,246,0.3)',
                        borderRadius: '8px', padding: '2px 7px', color: 'var(--text-secondary)'
                      }}>
                        {t.name.replace(`mcp_${s.name}_`, '')}
                      </span>
                    ))}
                  </div>
                )}
                <div style={{ marginTop: '6px', fontSize: '0.72rem', color: 'var(--text-secondary)', fontFamily: 'monospace', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                  {s.transport === 'http' ? s.url : s.command}
                </div>
                {s.lastError && (
                  <div style={{ marginTop: '8px', fontSize: '0.72rem', color: '#f87171', padding: '6px 8px', background: 'rgba(248,113,113,0.08)', borderRadius: '8px', border: '1px solid rgba(248,113,113,0.2)' }}>
                    <strong>Error:</strong> {s.lastError}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}

      {(!servers || servers.length === 0) && !adding && (
        <div style={{ color: 'var(--text-secondary)', fontSize: '0.85rem', padding: '12px 0' }}>
          No MCP servers configured. Click <strong>+ Add Server</strong> to connect one.
        </div>
      )}

      <button onClick={fetchMCP} style={{
        fontSize: '0.75rem', color: 'var(--text-secondary)', background: 'transparent', border: 'none', cursor: 'pointer', padding: '0'
      }}>↺ Refresh</button>
    </section>
  );
}

export default function Settings({ onClose }) {
  const [bearerToken, setBearerToken] = useState('');
  const [allModels, setAllModels] = useState([]);
  const [featuredModels, setFeaturedModels] = useState([]);
  const [defaultModel, setDefaultModel] = useState('');
  const [embeddingApiUrl, setEmbeddingApiUrl] = useState('');
  const [embeddingModel, setEmbeddingModel] = useState('');
  const [autoCompactTokens, setAutoCompactTokens] = useState(80000);
  const [agentOSEnabled, setAgentOSEnabled] = useState(true);
  const [agentOSPolicyEnforcement, setAgentOSPolicyEnforcement] = useState('deny_default');
  const [agentOSKillSwitch, setAgentOSKillSwitch] = useState(false);
  
  const [modelSearch, setModelSearch] = useState('');
  const [isSaving, setIsSaving] = useState(false);
  const [revealToken, setRevealToken] = useState(false);
  const [externalDefaultAgentId, setExternalDefaultAgentId] = useState('');

  useEffect(() => {
    const loadData = async () => {
      try {
        const [modelsRes, settingsRes] = await Promise.all([
          authFetch('/api/models?all=1'),
          authFetch('/api/settings')
        ]);
        
        const modelsData = await modelsRes.json();
        const settingsData = await settingsRes.json();

        if (modelsData && modelsData.data) {
          setAllModels(modelsData.data);
        }

        if (settingsData) {
          setBearerToken(getSessionToken());
          setFeaturedModels(settingsData.featured_models || []);
          setDefaultModel(settingsData.default_model || '');
          setEmbeddingApiUrl(settingsData.embedding_api_url || 'http://localhost:11434');
          setEmbeddingModel(settingsData.embedding_model || 'nomic-embed-text');
          setAutoCompactTokens(settingsData.auto_compact_tokens || 80000);
          setAgentOSEnabled(settingsData.agentos_enabled !== false);
          setAgentOSPolicyEnforcement(settingsData.agentos_policy_enforcement || 'deny_default');
          setAgentOSKillSwitch(Boolean(settingsData.agentos_kill_switch));
          setExternalDefaultAgentId(settingsData.external_default_agent_id || '');
        }
      } catch (err) {
        console.error("Failed to load settings data", err);
      }
    };
    loadData();
  }, []);

  const handleSave = async () => {
    setIsSaving(true);
    try {
      await authFetch('/api/settings', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json'
        },
        body: JSON.stringify({
          featured_models: featuredModels,
          default_model: defaultModel,
          embedding_api_url: embeddingApiUrl,
          embedding_model: embeddingModel,
          auto_compact_tokens: parseInt(autoCompactTokens) || 80000,
          agentos_enabled: agentOSEnabled,
          agentos_policy_enforcement: agentOSPolicyEnforcement,
          agentos_kill_switch: agentOSKillSwitch,
          external_default_agent_id: externalDefaultAgentId
        })
      });
      alert('Settings saved successfully!');
    } catch {
      alert('Failed to save settings');
    } finally {
      setIsSaving(false);
    }
  };

  const toggleFeatured = (modelId) => {
    setFeaturedModels(prev => 
      prev.includes(modelId) ? prev.filter(id => id !== modelId) : [...prev, modelId]
    );
  };

  const filteredModels = (allModels || []).filter(m => 
    m?.id?.toLowerCase().includes((modelSearch || '').toLowerCase())
  );

  // Deduplicate default model options to feature ONLY selected featured models (or all if none selected)
  const defaultModelOptions = featuredModels.length > 0 
    ? (allModels || []).filter(m => m?.id && featuredModels.includes(m.id))
    : (allModels || []);

  const externalApiUrl = window.location.origin + '/api/ext';

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal-content settings-modal" onClick={e => e.stopPropagation()} style={{ padding: '24px', position: 'relative' }}>
        
        <button 
          onClick={onClose} 
          style={{ position: 'absolute', top: '16px', right: '16px', background: 'transparent', color: 'var(--text-secondary)', border: 'none', fontSize: '1.2rem', padding: '4px', cursor: 'pointer' }}
          aria-label="Close"
        >
          &times;
        </button>

        <h2 style={{ marginBottom: '24px', paddingBottom: '12px', borderBottom: '1px solid var(--border-color)' }}>
          AgentHQ Settings
        </h2>

      <section style={{ marginBottom: '32px' }}>
        <h3 className="card-title">Agent OS Runtime</h3>
        <p style={{ color: 'var(--text-secondary)', fontSize: '0.9rem', marginBottom: '16px' }}>
          Configure global AgentHQ behavior. Company folder mappings are managed directly on each company.
        </p>

        <div style={{ display: 'grid', gap: '12px' }}>
          <label style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '12px' }}>
            <span style={{ fontSize: '0.9rem', color: 'var(--text-primary)' }}>Enable Agent OS</span>
            <input
              type="checkbox"
              checked={agentOSEnabled}
              onChange={(e) => setAgentOSEnabled(e.target.checked)}
              style={{ width: '18px', height: '18px', accentColor: 'var(--accent-color)' }}
            />
          </label>

          <div>
            <label style={{ display: 'block', fontSize: '0.85rem', color: 'var(--text-secondary)', marginBottom: '4px' }}>Policy Enforcement</label>
            <select value={agentOSPolicyEnforcement} onChange={(e) => setAgentOSPolicyEnforcement(e.target.value)}>
              <option value="deny_default">deny_default</option>
              <option value="allow_default">allow_default</option>
            </select>
          </div>

          <label style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '12px' }}>
            <span style={{ fontSize: '0.9rem', color: 'var(--text-primary)' }}>Emergency Kill Switch</span>
            <input
              type="checkbox"
              checked={agentOSKillSwitch}
              onChange={(e) => setAgentOSKillSwitch(e.target.checked)}
              style={{ width: '18px', height: '18px', accentColor: 'var(--accent-color)' }}
            />
          </label>
        </div>
      </section>

      {/* MCP Servers */}
      <MCPPanel />

      {/* External Integration Instructions */}
      <section style={{ marginBottom: '32px' }}>
        <h3 className="card-title">External API Integration</h3>
        <p style={{ color: 'var(--text-secondary)', fontSize: '0.9rem', marginBottom: '16px' }}>
          Use these credentials to connect 3rd party applications like VSCode Extensions (Cline, Continue.dev) or Mobile Apps directly to your local models and OpenRouter proxies.
        </p>

        <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
          <div>
            <label style={{ display: 'block', fontSize: '0.85rem', color: 'var(--text-secondary)', marginBottom: '4px' }}>Base URL / API Endpoint</label>
            <input type="text" readOnly value={externalApiUrl} style={{ backgroundColor: 'var(--bg-color)', color: 'var(--text-primary)' }} />
          </div>

          <div>
            <label style={{ display: 'block', fontSize: '0.85rem', color: 'var(--text-secondary)', marginBottom: '4px' }}>Bearer Token (Current Session)</label>
            <div style={{ display: 'flex', gap: '8px' }}>
              <input
                type={revealToken ? "text" : "password"}
                readOnly
                value={bearerToken}
                style={{ backgroundColor: 'var(--bg-color)', color: 'var(--text-primary)', flex: 1 }}
              />
              <button className="btn-secondary" onClick={() => setRevealToken(!revealToken)}>
                {revealToken ? "Hide" : "Reveal"}
              </button>
            </div>
          </div>

          <div>
            <label style={{ display: 'block', fontSize: '0.85rem', color: 'var(--text-secondary)', marginBottom: '4px' }}>Default Manager Agent ID</label>
            <input
              type="text"
              value={externalDefaultAgentId}
              onChange={(e) => setExternalDefaultAgentId(e.target.value)}
              placeholder="Agent UUID to use when no X-Agent-ID header is set"
              style={{ width: '100%' }}
            />
            <p style={{ fontSize: '0.78rem', color: 'var(--text-secondary)', marginTop: '4px' }}>
              Use <code>X-Agent-ID: &lt;agent-uuid&gt;</code> header to route a specific request to a different manager.
            </p>
          </div>

          <div>
            <label style={{ display: 'block', fontSize: '0.85rem', color: 'var(--text-secondary)', marginBottom: '4px' }}>Example Request</label>
            <pre style={{ background: 'var(--bg-color)', color: 'var(--text-primary)', padding: '10px 14px', borderRadius: '8px', fontSize: '0.78rem', overflowX: 'auto', margin: 0 }}>{`curl ${window.location.origin}/api/ext/chat/completions \\
  -H "Authorization: Bearer <token>" \\
  -H "X-Agent-ID: <agent-uuid>" \\
  -H "Content-Type: application/json" \\
  -d '{"model":"","messages":[{"role":"user","content":"Hello"}]}'`}</pre>
          </div>
        </div>
      </section>

      {/* RAG Memory Settings */}
      <section style={{ marginBottom: '32px' }}>
        <h3 className="card-title">Episodic RAG Memory</h3>
        <p style={{ color: 'var(--text-secondary)', fontSize: '0.9rem', marginBottom: '16px' }}>
          Configure embeddings for episodic memory. Use local Ollama (`http://localhost:11434` + `nomic-embed-text`) or OpenRouter (`https://openrouter.ai/api/v1` + an embedding model) when `OPENROUTER_API_KEY` is set.
        </p>

        <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
          <div>
            <label style={{ display: 'block', fontSize: '0.85rem', color: 'var(--text-secondary)', marginBottom: '4px' }}>Embedding API URL</label>
            <input 
               type="text" 
               value={embeddingApiUrl} 
               onChange={(e) => setEmbeddingApiUrl(e.target.value)}
               placeholder="http://localhost:11434 or https://openrouter.ai/api/v1"
            />
          </div>

          <div>
            <label style={{ display: 'block', fontSize: '0.85rem', color: 'var(--text-secondary)', marginBottom: '4px' }}>Text Embedding Model</label>
            <input 
               type="text" 
               value={embeddingModel} 
               onChange={(e) => setEmbeddingModel(e.target.value)}
               placeholder="nomic-embed-text"
            />
          </div>
        </div>
      </section>

      {/* Context Compaction Settings */}
      <section style={{ marginBottom: '32px' }}>
        <h3 className="card-title">Context Compaction Threshold</h3>
        <p style={{ color: 'var(--text-secondary)', fontSize: '0.9rem', marginBottom: '16px' }}>
          When a chat gets too long, AgentHQ automatically summarizes the middle portion to save tokens and prevent forgetting instructions. This sets how many tokens the conversation must consume before triggering compaction. Higher threshold = higher cost but longer verbatim memory.
        </p>

        <div>
          <label style={{ display: 'block', fontSize: '0.85rem', color: 'var(--text-secondary)', marginBottom: '4px' }}>Compaction Token Limit</label>
          <input 
             type="number" 
             value={autoCompactTokens} 
             onChange={(e) => setAutoCompactTokens(e.target.value)}
             min="1000"
             max="500000"
             step="1000"
             style={{ width: '100%', padding: '10px', borderRadius: '8px', backgroundColor: 'var(--bg-color)', color: 'var(--text-primary)', border: '1px solid var(--border-color)' }}
          />
        </div>
      </section>

      {/* Default Model */}
      <section style={{ marginBottom: '32px' }}>
        <h3 className="card-title">Default Model</h3>
        <p style={{ color: 'var(--text-secondary)', fontSize: '0.9rem', marginBottom: '12px' }}>
          This is the fallback model used when external applications do not specify one.
        </p>
        <select 
          value={defaultModel} 
          onChange={(e) => setDefaultModel(e.target.value)}
          style={{ width: '100%', padding: '10px', borderRadius: '8px', backgroundColor: 'var(--bg-color)', color: 'var(--text-primary)', border: '1px solid var(--border-color)' }}
        >
          <option value="">-- No Default (OpenRouter Native Fallback) --</option>
          {defaultModelOptions.map(m => (
            <option key={m.id} value={m.id}>{m.name || m.id}</option>
          ))}
        </select>
      </section>

      {/* Featured Models Selector */}
      <section style={{ marginBottom: '24px' }}>
        <h3 className="card-title">Featured Models</h3>
        <p style={{ color: 'var(--text-secondary)', fontSize: '0.9rem', marginBottom: '12px' }}>
          Select specific models to pin. If empty, all ~350 models will load. If selected, only these models will be available in the Web UI and through the External API.
        </p>
        
        <input 
          type="text" 
          placeholder="Search all openrouter & local models..." 
          value={modelSearch}
          onChange={(e) => setModelSearch(e.target.value)}
          style={{ marginBottom: '12px', backgroundColor: 'var(--bg-color)', color: 'var(--text-primary)' }}
        />

        <div style={{ display: 'flex', flexDirection: 'column', gap: '8px', maxHeight: '400px', overflowY: 'auto', border: '1px solid var(--border-color)', borderRadius: '8px', padding: '12px' }}>
          {filteredModels.map(m => {
            const isFeatured = featuredModels.includes(m.id);
            return (
              <label key={m.id} style={{ display: 'flex', alignItems: 'center', gap: '12px', cursor: 'pointer', padding: '4px', borderRadius: '8px', backgroundColor: isFeatured ? 'var(--surface-hover)' : 'transparent' }}>
                <input 
                  type="checkbox" 
                  checked={isFeatured}
                  onChange={() => toggleFeatured(m.id)}
                  style={{ width: '16px', height: '16px', accentColor: 'var(--accent-color)' }}
                />
                <span style={{ fontSize: '0.95rem' }}>{m.name || m.id}</span>
                <span style={{ fontSize: '0.75rem', color: 'var(--text-secondary)', marginLeft: 'auto' }}>{m.id}</span>
              </label>
            );
          })}
        </div>
      </section>

        <div style={{ display: 'flex', gap: '12px', marginTop: '16px' }}>
          <button className="btn-secondary" onClick={onClose} style={{ flex: 1 }}>
            Close
          </button>
          <button onClick={handleSave} disabled={isSaving} style={{ flex: 1 }}>
            {isSaving ? 'Saving...' : 'Save Settings'}
          </button>
        </div>

      </div>
    </div>
  );
}
