---
description: LLM API integration — streaming SSE, tool/function calling, prompt engineering, token management, OpenAI-compatible APIs
---

# LLM Integration Skill

## Streaming SSE (Server-Sent Events) Pattern

### Backend (Go)
```go
w.Header().Set("Content-Type", "text/event-stream")
w.Header().Set("Cache-Control", "no-cache")
w.Header().Set("Connection", "keep-alive")
flusher, _ := w.(http.Flusher)

// Stream each chunk
w.Write([]byte("data: " + jsonChunk + "\n\n"))
flusher.Flush()
// Terminate:
w.Write([]byte("data: [DONE]\n\n"))
```

### Frontend (React)
```js
const response = await fetch('/api/chat', { method: 'POST', signal: controller.signal, body: ... });
const reader = response.body.getReader();
const decoder = new TextDecoder();
let buffer = '';
while (true) {
  const { value, done } = await reader.read();
  if (done) break;
  buffer += decoder.decode(value, { stream: true });
  for (const line of buffer.split('\n')) {
    if (line.startsWith('data: ') && line !== 'data: [DONE]') {
      const chunk = JSON.parse(line.slice(6));
      const delta = chunk.choices[0]?.delta?.content ?? '';
      setOutput(prev => prev + delta);
    }
  }
  buffer = buffer.split('\n').pop(); // keep incomplete line
}
```

## Tool/Function Calling

### Tool Definition Schema
```json
{
  "type": "function",
  "function": {
    "name": "read_file",
    "description": "Reads the content of a file. Use this to understand existing code before making changes.",
    "parameters": {
      "type": "object",
      "properties": {
        "path": { "type": "string", "description": "File path relative to workspace root" }
      },
      "required": ["path"]
    }
  }
}
```

### Tool Loop Pattern (Go)
```go
for attempt := 0; attempt < maxAttempts; attempt++ {
    resp := callLLM(messages, tools)
    if resp.ToolCalls == nil { break } // No tools called → final response

    messages = append(messages, resp.AsAssistantMessage())
    for _, tc := range resp.ToolCalls {
        result := executeTool(tc.Name, tc.Arguments)
        messages = append(messages, toolResultMessage(tc.ID, tc.Name, result))
    }
}
```

## Token Management

### Rough estimates
- 1 token ≈ 4 characters (English)
- 1 token ≈ 0.75 words
- A typical code file of 100 lines ≈ 800–1200 tokens
- GPT-4o context: 128k tokens; Claude 3.7: 200k tokens

### Context compression trigger
```
if estimatedTokens > 80_000:
    summarize middle messages → replace with summary
    keep: system prompt + first 2 turns + last 4 turns
```

## Prompt Engineering Principles

### Chain of Thought
Add to system prompt: *"Before answering, think step by step. Write your reasoning, then your final answer."*

### Few-Shot Examples
```
User: Convert 5 miles to km
Assistant: 5 × 1.60934 = 8.0467 km

User: Convert 10 miles to km  
Assistant:
```

### Structured Output
Request JSON and validate:
```
Respond only with valid JSON matching this schema: {"action": "string", "confidence": 0-1, "reasoning": "string"}
Do not include any text outside the JSON object.
```

### Persona + Constraints
```
You are a senior Go engineer. You MUST:
- Always run go vet after editing
- Never use global variables
- Return errors — never panic
- Write table-driven tests
```

## OpenRouter-Specific Notes
- `X-Title` header for dashboard tracking
- Model IDs: `anthropic/claude-3.7-sonnet`, `google/gemini-2.0-flash`, `openai/gpt-4o`
- Streaming: same as OpenAI (`stream: true` in payload)
- Tool calling: same schema as OpenAI function calling spec
- Some models don't support tools — check `supported_parameters` from `/api/v1/models`
