# Apollo

Apollo is a self-hosted dashboard for building and operating AI companies. It combines a Go backend with a React frontend for agent organization, tasks, memory, MCP tools, schedules, governance, and audit history.

## Project layout

```text
dash/
├── backend/    Go API, SQLite persistence, workers, and static file server
└── frontend/   React + Vite dashboard
```

## Requirements

- Go 1.25.6 or newer
- Node.js 20 or newer
- npm

## Local development

1. Create local backend configuration:

   ```bash
   cp dash/backend/.env.example dash/backend/.env
   ```

   Set `DASHBOARD_PASSWORD` and add an `OPENROUTER_API_KEY` if you want to use hosted models. Keep `.env` local; it is ignored by Git.

2. Install frontend dependencies and build the dashboard:

   ```bash
   cd dash/frontend
   npm ci
   npm run build
   ```

3. Start the backend:

   ```bash
   cd dash/backend
   go run .
   ```

   The dashboard is available at `http://localhost:4000`.

For frontend hot reload, run `npm run dev` in `dash/frontend` and keep the backend running on port 4000. The Vite proxy forwards `/api` and `/ws` requests to the backend.

## Verification

Run the same checks used by CI:

```bash
cd dash/backend
go test ./...

cd ../frontend
npm run lint
npm run build
```

## Production build

`dash/backend/build.sh` cross-compiles Linux AMD64 and ARM64 backend binaries with Zig, then builds the frontend. Install Zig first:

```bash
brew install zig
cd dash/backend
./build.sh
```

The generated binaries, frontend bundle, database, runtime workspace, and credentials are intentionally excluded from version control.

## Configuration

The main backend settings are documented in `dash/backend/.env.example`:

- `PORT` — HTTP port, default `4000`
- `DASHBOARD_PASSWORD` — admin password for protected routes
- `OPENROUTER_API_KEY` — hosted model and embedding access
- `OPENAI_API_KEY` — optional OpenAI-compatible fallback
- `RESEND_API_KEY` and `APP_URL` — optional email notifications
- `VULTA_API_KEY` and `VULTA_WEBHOOK_SECRET` — optional billing integration
- `AGENTHQ_WORKSPACE_ROOT` — optional override for workspace storage
- `OLLAMA_API_URL` — optional local Ollama-compatible endpoint
- `APOLLO_DEBUG_SYSTEM_LOG` — optional verbose model prompt logging
- `CONTEXTPLUS_EMBED_TRACKER` — optional embedding tracking toggle

Never commit real credentials, private keys, SQLite databases, or built binaries.
