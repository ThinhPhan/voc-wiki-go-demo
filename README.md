# voc-wiki-go-demo

A simple wiki written in Go, backed by SQLite, packaged with Docker.

## Features

- 📝 Create and edit wiki pages
- 🔗 `[[Wiki Links]]` — link pages with double-bracket syntax
- 💾 SQLite database stored at `/storage/wiki.db`
- ✅ Health check endpoint at `GET /up` → `200 OK`
- 🐳 Docker + Docker Compose ready

## Quick Start

### With Docker Compose

```bash
docker compose up --build
```

Then open [http://localhost:8080](http://localhost:8080).

### Without Docker (requires Go 1.22+ and gcc)

```bash
go run .
```

## Routes

| Route | Description |
|-------|-------------|
| `GET /` | Redirects to `/w/home` |
| `GET /w/:slug` | View a wiki page |
| `GET /edit/:slug` | Edit (or create) a page |
| `POST /save/:slug` | Save a page |
| `GET /pages` | List all pages |
| `GET /up` | Health check — returns `200 OK` |

## Wiki Syntax

| Syntax | Result |
|--------|--------|
| `[[Page Name]]` | Link to a wiki page |
| `**bold**` | **Bold** text |
| `- item` | Bullet list item |

## Storage

The SQLite database is stored at `/storage/wiki.db`. Mount a volume there to persist data:

```bash
docker run -p 8080:8080 -v $(pwd)/data:/storage voc-wiki
```

## Configuration

| Environment | Default | Description |
|-------------|---------|-------------|
| — | `:8080` | Listening port (hardcoded; override with a proxy) |
