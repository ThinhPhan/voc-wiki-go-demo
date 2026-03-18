package main

import (
	"database/sql"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const dbPath = "/storage/wiki.db"

var db *sql.DB

// linkPattern matches [[PageName]] or [[Page Name]] style wiki links
var linkPattern = regexp.MustCompile(`\[\[([^\]]+)\]\]`)

// newlinePattern converts newlines to <br> in content
var newlinePattern = regexp.MustCompile(`\r?\n`)

func main() {
	// Ensure storage directory exists
	if err := os.MkdirAll("/storage", 0755); err != nil {
		log.Fatalf("failed to create /storage: %v", err)
	}

	var err error
	db, err = sql.Open("sqlite3", dbPath+"?_journal_mode=WAL")
	if err != nil {
		log.Fatalf("failed to open sqlite db: %v", err)
	}
	defer db.Close()

	if err := initDB(); err != nil {
		log.Fatalf("failed to init db: %v", err)
	}

	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("/up", handleUp)

	// Wiki routes
	mux.HandleFunc("/", handleIndex)
	mux.HandleFunc("/w/", handleWiki)
	mux.HandleFunc("/edit/", handleEdit)
	mux.HandleFunc("/save/", handleSave)
	mux.HandleFunc("/pages", handlePageList)

	addr := ":8080"
	log.Printf("voc-wiki listening on %s", addr)
	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func initDB() error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS pages (
			slug    TEXT PRIMARY KEY,
			title   TEXT NOT NULL,
			content TEXT NOT NULL DEFAULT '',
			updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
		)
	`)
	if err != nil {
		return fmt.Errorf("create pages table: %w", err)
	}

	// Seed a welcome page if empty
	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM pages").Scan(&count)
	if count == 0 {
		_, _ = db.Exec(`INSERT INTO pages (slug, title, content) VALUES (?, ?, ?)`,
			"home",
			"Home",
			"Welcome to **voc-wiki**!\n\nTry creating a new page or follow a [[link]].\n\nSome starter links:\n- [[Getting Started]]\n- [[About]]",
		)
	}
	return nil
}

// ── Handlers ──────────────────────────────────────────────────────────────────

func handleUp(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "OK")
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" {
		http.Redirect(w, r, "/w/home", http.StatusFound)
		return
	}
	http.NotFound(w, r)
}

func handleWiki(w http.ResponseWriter, r *http.Request) {
	slug := slugFromPath(r.URL.Path, "/w/")
	if slug == "" {
		http.Redirect(w, r, "/w/home", http.StatusFound)
		return
	}

	page, err := getPage(slug)
	if err != nil {
		// Page not found → redirect to editor
		http.Redirect(w, r, "/edit/"+slug, http.StatusFound)
		return
	}

	renderTemplate(w, "view", page)
}

func handleEdit(w http.ResponseWriter, r *http.Request) {
	slug := slugFromPath(r.URL.Path, "/edit/")
	if slug == "" {
		http.Redirect(w, r, "/w/home", http.StatusFound)
		return
	}

	page, _ := getPage(slug)
	if page == nil {
		page = &Page{Slug: slug, Title: titleFromSlug(slug), Content: ""}
	}
	renderTemplate(w, "edit", page)
}

func handleSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	slug := slugFromPath(r.URL.Path, "/save/")
	if slug == "" {
		http.Error(w, "bad slug", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	title := strings.TrimSpace(r.FormValue("title"))
	content := r.FormValue("content")
	if title == "" {
		title = titleFromSlug(slug)
	}

	if err := savePage(slug, title, content); err != nil {
		http.Error(w, "save failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/w/"+slug, http.StatusSeeOther)
}

func handlePageList(w http.ResponseWriter, r *http.Request) {
	pages, err := listPages()
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	renderTemplate(w, "list", pages)
}

// ── DB helpers ─────────────────────────────────────────────────────────────

type Page struct {
	Slug      string
	Title     string
	Content   string
	UpdatedAt string
}

func getPage(slug string) (*Page, error) {
	row := db.QueryRow("SELECT slug, title, content, updated_at FROM pages WHERE slug = ?", slug)
	p := &Page{}
	if err := row.Scan(&p.Slug, &p.Title, &p.Content, &p.UpdatedAt); err != nil {
		return nil, err
	}
	return p, nil
}

func savePage(slug, title, content string) error {
	_, err := db.Exec(`
		INSERT INTO pages (slug, title, content, updated_at)
		VALUES (?, ?, ?, datetime('now'))
		ON CONFLICT(slug) DO UPDATE SET
			title = excluded.title,
			content = excluded.content,
			updated_at = excluded.updated_at
	`, slug, title, content)
	return err
}

func listPages() ([]Page, error) {
	rows, err := db.Query("SELECT slug, title, updated_at FROM pages ORDER BY updated_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var pages []Page
	for rows.Next() {
		var p Page
		if err := rows.Scan(&p.Slug, &p.Title, &p.UpdatedAt); err != nil {
			return nil, err
		}
		pages = append(pages, p)
	}
	return pages, rows.Err()
}

// ── Utilities ──────────────────────────────────────────────────────────────

func slugFromPath(path, prefix string) string {
	s := strings.TrimPrefix(path, prefix)
	s = strings.Trim(s, "/")
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	return s
}

func titleFromSlug(slug string) string {
	words := strings.Split(slug, "-")
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// renderWikiLinks converts [[Page Name]] into clickable <a> tags.
func renderWikiLinks(content string) template.HTML {
	// Escape HTML first, then apply wiki link replacement
	escaped := template.HTMLEscapeString(content)

	// Convert wiki links
	result := linkPattern.ReplaceAllStringFunc(escaped, func(match string) string {
		inner := match[2 : len(match)-2] // strip [[ and ]]
		slug := strings.ToLower(strings.ReplaceAll(inner, " ", "-"))
		return fmt.Sprintf(`<a href="/w/%s" class="wiki-link">%s</a>`, slug, inner)
	})

	// Convert simple **bold**
	boldPattern := regexp.MustCompile(`\*\*(.+?)\*\*`)
	result = boldPattern.ReplaceAllString(result, `<strong>$1</strong>`)

	// Convert newlines to <br>
	result = newlinePattern.ReplaceAllString(result, "<br>")

	// Convert - list items
	lines := strings.Split(result, "<br>")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- ") {
			lines[i] = "<li>" + strings.TrimPrefix(trimmed, "- ") + "</li>"
		}
	}
	result = strings.Join(lines, "\n")

	return template.HTML(result)
}

// ── Templates ──────────────────────────────────────────────────────────────

func renderTemplate(w http.ResponseWriter, name string, data interface{}) {
	tmpl, err := template.New("").Funcs(template.FuncMap{
		"wikiLinks": renderWikiLinks,
	}).Parse(baseTemplate + templates[name])
	if err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		log.Printf("template exec error: %v", err)
	}
}

const baseTemplate = `
{{define "base"}}
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>voc-wiki</title>
  <style>
    *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      font-family: 'Segoe UI', system-ui, sans-serif;
      background: #0f1117;
      color: #e2e8f0;
      min-height: 100vh;
      display: flex;
      flex-direction: column;
    }
    header {
      background: linear-gradient(135deg, #1a1f35 0%, #0f1117 100%);
      border-bottom: 1px solid #2d3748;
      padding: 1rem 2rem;
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 1rem;
    }
    header a.logo {
      font-size: 1.5rem;
      font-weight: 700;
      color: #7c3aed;
      text-decoration: none;
      letter-spacing: -0.5px;
    }
    header a.logo span { color: #a78bfa; }
    nav { display: flex; gap: 1rem; align-items: center; }
    nav a {
      color: #94a3b8;
      text-decoration: none;
      font-size: 0.9rem;
      padding: 0.35rem 0.75rem;
      border-radius: 6px;
      transition: background 0.15s, color 0.15s;
    }
    nav a:hover { background: #1e293b; color: #e2e8f0; }
    .btn {
      display: inline-block;
      padding: 0.45rem 1rem;
      border-radius: 8px;
      font-size: 0.9rem;
      font-weight: 600;
      text-decoration: none;
      cursor: pointer;
      border: none;
      transition: all 0.15s;
    }
    .btn-primary { background: #7c3aed; color: #fff; }
    .btn-primary:hover { background: #6d28d9; }
    .btn-secondary { background: #1e293b; color: #94a3b8; border: 1px solid #2d3748; }
    .btn-secondary:hover { background: #2d3748; color: #e2e8f0; }
    main {
      flex: 1;
      max-width: 860px;
      width: 100%;
      margin: 2rem auto;
      padding: 0 1.5rem;
    }
    .card {
      background: #1a1f35;
      border: 1px solid #2d3748;
      border-radius: 12px;
      padding: 2rem;
    }
    h1 { font-size: 1.8rem; font-weight: 700; color: #f1f5f9; margin-bottom: 0.25rem; }
    .meta { color: #64748b; font-size: 0.8rem; margin-bottom: 1.5rem; }
    .content { line-height: 1.8; color: #cbd5e1; }
    .content a.wiki-link {
      color: #a78bfa;
      text-decoration: none;
      border-bottom: 1px dashed #7c3aed;
      transition: color 0.15s;
    }
    .content a.wiki-link:hover { color: #c4b5fd; border-bottom-color: #a78bfa; }
    .content li { margin-left: 1.5rem; margin-top: 0.25rem; }
    .actions { display: flex; gap: 0.75rem; margin-top: 1.5rem; padding-top: 1.5rem; border-top: 1px solid #2d3748; }
    form label { display: block; color: #94a3b8; font-size: 0.85rem; margin-bottom: 0.4rem; margin-top: 1rem; }
    form input[type=text], form textarea {
      width: 100%;
      background: #0f1117;
      border: 1px solid #2d3748;
      border-radius: 8px;
      color: #e2e8f0;
      padding: 0.65rem 0.9rem;
      font-size: 0.95rem;
      font-family: inherit;
      outline: none;
      transition: border-color 0.15s;
    }
    form input[type=text]:focus, form textarea:focus { border-color: #7c3aed; }
    form textarea { min-height: 320px; resize: vertical; line-height: 1.7; }
    .hint { font-size: 0.78rem; color: #475569; margin-top: 0.3rem; }
    table { width: 100%; border-collapse: collapse; margin-top: 1rem; }
    th { text-align: left; color: #64748b; font-size: 0.8rem; text-transform: uppercase; letter-spacing: 0.05em; padding: 0.5rem 0.75rem; border-bottom: 1px solid #2d3748; }
    td { padding: 0.65rem 0.75rem; border-bottom: 1px solid #1e293b; }
    td a { color: #a78bfa; text-decoration: none; }
    td a:hover { color: #c4b5fd; }
    .badge { display: inline-block; padding: 0.2rem 0.55rem; border-radius: 999px; font-size: 0.72rem; background: #1e293b; color: #64748b; }
    footer { text-align: center; color: #334155; font-size: 0.78rem; padding: 1.5rem; margin-top: auto; }
  </style>
</head>
<body>
  <header>
    <a href="/" class="logo">voc<span>-wiki</span></a>
    <nav>
      <a href="/pages">All Pages</a>
      <a href="/edit/new-page" class="btn btn-primary">+ New Page</a>
    </nav>
  </header>
  <main>
    {{template "content" .}}
  </main>
  <footer>voc-wiki · powered by Go + SQLite</footer>
</body>
</html>
{{end}}
`

var templates = map[string]string{
	"view": `
{{define "content"}}
<div class="card">
  <h1>{{.Title}}</h1>
  <p class="meta">Last updated: {{.UpdatedAt}}</p>
  <div class="content">{{wikiLinks .Content}}</div>
  <div class="actions">
    <a href="/edit/{{.Slug}}" class="btn btn-secondary">✏️ Edit</a>
    <a href="/pages" class="btn btn-secondary">📄 All Pages</a>
  </div>
</div>
{{end}}
`,
	"edit": `
{{define "content"}}
<div class="card">
  <h1>{{if .Content}}Edit{{else}}New Page{{end}}: {{.Title}}</h1>
  <form method="POST" action="/save/{{.Slug}}">
    <label for="title">Page Title</label>
    <input id="title" type="text" name="title" value="{{.Title}}" placeholder="Page title" required>
    <label for="content">Content</label>
    <textarea id="content" name="content" placeholder="Write your page content here...">{{.Content}}</textarea>
    <p class="hint">Use [[Page Name]] to link to other wiki pages. Use **bold** for emphasis.</p>
    <div class="actions">
      <button type="submit" class="btn btn-primary">💾 Save</button>
      <a href="/w/{{.Slug}}" class="btn btn-secondary">Cancel</a>
    </div>
  </form>
</div>
{{end}}
`,
	"list": `
{{define "content"}}
<div class="card">
  <h1>All Pages</h1>
  <table>
    <thead><tr><th>Page</th><th>Slug</th><th>Updated</th></tr></thead>
    <tbody>
    {{range .}}
    <tr>
      <td><a href="/w/{{.Slug}}">{{.Title}}</a></td>
      <td><span class="badge">{{.Slug}}</span></td>
      <td>{{.UpdatedAt}}</td>
    </tr>
    {{else}}
    <tr><td colspan="3">No pages yet. <a href="/edit/home">Create the first one!</a></td></tr>
    {{end}}
    </tbody>
  </table>
</div>
{{end}}
`,
}
