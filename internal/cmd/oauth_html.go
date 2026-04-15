package cmd

import (
	"bytes"
	_ "embed"
	"html/template"
	"log/slog"
	"net/http"
)

//go:embed oauth_assets/openbindings-glyph.svg
var oauthOpenBindingsGlyph string

// oauthConsentView drives the HTML consent template (Glyph is trusted SVG from embed).
type oauthConsentView struct {
	ClientID string
	Nonce    string
	Glyph    template.HTML
}

// oauthErrorView drives the authorization error HTML page.
type oauthErrorView struct {
	Title       string
	Heading     string
	Description string
	ErrCode     string
	Glyph       template.HTML
}

var oauthPages = template.Must(template.New("oauth").Parse(`
{{define "oauth-shared-head"}}
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light dark">
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600&display=swap" rel="stylesheet">
{{end}}

{{define "oauth-shared-css"}}
<style>
  :root {
    --page-bg: #fafafa;
    --color-bg: #ffffff;
    --color-surface: #ffffff;
    --color-text: #000000;
    --color-text-secondary: #666666;
    --color-text-tertiary: #999999;
    --color-border: #f0f0f0;
    --color-fill: #f0f0f0;
    --color-active: #000000;
    --color-error: #dc2626;
    --radius-md: 8px;
    --radius-lg: 12px;
    --focus-ring: color-mix(in srgb, var(--color-active) 55%, var(--color-border));
    --duration-fast: 0.12s;
  }
  @media (prefers-color-scheme: dark) {
    :root {
      --page-bg: #0a0a0a;
      --color-bg: #0a0a0a;
      --color-surface: #0a0a0a;
      --color-text: #e5e5e5;
      --color-text-secondary: #a3a3a3;
      --color-text-tertiary: #8a8a8a;
      --color-fill: #171717;
      --color-border: #262626;
      --color-active: #e5e5e5;
      --color-error: #f87171;
    }
  }
  *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
  body {
    font-family: 'Inter', system-ui, -apple-system, sans-serif;
    background: var(--page-bg);
    color: var(--color-text);
    -webkit-font-smoothing: antialiased;
    -moz-osx-font-smoothing: grayscale;
    min-height: 100dvh;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 1.5rem;
  }
  .page { width: 100%; max-width: 26rem; }
  .brand {
    display: flex;
    align-items: center;
    gap: 0.85rem;
    margin-bottom: 1.25rem;
  }
  .brand-mark {
    flex-shrink: 0;
    width: 2.5rem;
    height: 2.85rem;
    color: var(--color-text);
  }
  .brand-mark :is(svg) { width: 100%; height: 100%; display: block; }
  .brand-text { min-width: 0; }
  .brand-name {
    font-size: 1.125rem;
    font-weight: 600;
    line-height: 1.25;
    letter-spacing: -0.02em;
  }
  .brand-sub {
    font-size: 0.72rem;
    font-weight: 500;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--color-text-tertiary);
    margin-top: 0.2rem;
  }
  .card {
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-lg);
    padding: 1.35rem 1.35rem 1.25rem;
  }
  .card--error {
    border-left: 3px solid var(--color-error);
  }
  .card h1 {
    font-size: 0.9375rem;
    font-weight: 600;
    margin-bottom: 0.65rem;
  }
  .card p {
    font-size: 0.8125rem;
    line-height: 1.55;
    color: var(--color-text-secondary);
    margin-bottom: 1rem;
  }
  .card p:last-child { margin-bottom: 0; }
  .client {
    font-family: ui-monospace, 'JetBrains Mono', monospace;
    font-size: 0.78rem;
    font-weight: 500;
    color: var(--color-text);
    word-break: break-all;
  }
  .err-code {
    margin-top: 0.85rem;
    padding-top: 0.85rem;
    border-top: 1px solid var(--color-border);
    font-size: 0.72rem;
    color: var(--color-text-tertiary);
  }
  .err-code code {
    font-family: ui-monospace, 'JetBrains Mono', monospace;
    font-size: 0.75rem;
    color: var(--color-text-secondary);
    font-weight: 500;
  }
  .actions { display: flex; gap: 0.5rem; }
  button {
    flex: 1;
    padding: 0.55rem 0.85rem;
    border-radius: var(--radius-md);
    font-size: 0.8125rem;
    font-weight: 500;
    font-family: inherit;
    cursor: pointer;
    transition: background var(--duration-fast) ease, opacity var(--duration-fast) ease, border-color var(--duration-fast) ease;
  }
  button:focus:not(:focus-visible) { outline: none; }
  button:focus-visible {
    outline: 2px solid var(--focus-ring);
    outline-offset: 2px;
  }
  .approve {
    background: var(--color-active);
    color: var(--color-bg);
    border: 1px solid var(--color-active);
  }
  .approve:hover { opacity: 0.88; }
  .deny {
    background: transparent;
    color: var(--color-text-secondary);
    border: 1px solid var(--color-border);
  }
  .deny:hover {
    background: var(--color-fill);
    color: var(--color-text);
  }
  .footer {
    margin-top: 1rem;
    text-align: center;
    font-size: 0.6875rem;
    color: var(--color-text-tertiary);
  }
  .footer a {
    color: var(--color-text-secondary);
    text-decoration: none;
  }
  .footer a:hover { text-decoration: underline; }
</style>
{{end}}

{{define "oauth-consent"}}
<!DOCTYPE html>
<html lang="en">
<head>
{{template "oauth-shared-head"}}
<title>Authorize — OpenBindings</title>
{{template "oauth-shared-css"}}
</head>
<body>
<div class="page">
  <header class="brand">
    <div class="brand-mark">{{.Glyph}}</div>
    <div class="brand-text">
      <div class="brand-name">OpenBindings</div>
      <div class="brand-sub">ob serve · authorization</div>
    </div>
  </header>
  <div class="card">
    <h1>Authorize access</h1>
    <p><span class="client">{{.ClientID}}</span> is requesting access to this OpenBindings environment. Approving grants access to all operations, binding execution, and stored credentials for this local server.</p>
    <form method="POST" action="/oauth/authorize">
      <input type="hidden" name="nonce" value="{{.Nonce}}">
      <div class="actions">
        <button type="submit" name="action" value="deny" class="deny">Deny</button>
        <button type="submit" name="action" value="approve" class="approve">Approve</button>
      </div>
    </form>
  </div>
  <p class="footer"><a href="https://openbindings.com" target="_blank" rel="noopener noreferrer">openbindings.com</a></p>
</div>
</body>
</html>
{{end}}

{{define "oauth-error"}}
<!DOCTYPE html>
<html lang="en">
<head>
{{template "oauth-shared-head"}}
<title>{{.Title}}</title>
{{template "oauth-shared-css"}}
</head>
<body>
<div class="page">
  <header class="brand">
    <div class="brand-mark">{{.Glyph}}</div>
    <div class="brand-text">
      <div class="brand-name">OpenBindings</div>
      <div class="brand-sub">ob serve · authorization</div>
    </div>
  </header>
  <div class="card card--error">
    <h1>{{.Heading}}</h1>
    <p>{{.Description}}</p>
    {{if .ErrCode}}<p class="err-code">Error code: <code>{{.ErrCode}}</code></p>{{end}}
  </div>
  <p class="footer"><a href="https://openbindings.com" target="_blank" rel="noopener noreferrer">openbindings.com</a></p>
</div>
</body>
</html>
{{end}}
`))

func writeOAuthHTML(w http.ResponseWriter, status int, templateName string, data any, logger *slog.Logger) {
	var buf bytes.Buffer
	if err := oauthPages.ExecuteTemplate(&buf, templateName, data); err != nil {
		logger.Error("oauth html render failed", "template", templateName, "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

func oauthAuthorizeHTMLFail(w http.ResponseWriter, logger *slog.Logger, status int, heading, description, errCode string) {
	writeOAuthHTML(w, status, "oauth-error", oauthErrorView{
		Title:       "Authorization error — OpenBindings",
		Heading:     heading,
		Description: description,
		ErrCode:     errCode,
		Glyph:       template.HTML(oauthOpenBindingsGlyph),
	}, logger)
}
