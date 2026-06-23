package web

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strings"
)

type apiError struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(apiError{Error: message, Code: code})
}

// writeHTMLErrorPage renders a minimal standalone HTML error page. Used for
// failures that occur during full-page browser navigations (e.g. the OIDC
// callback redirect), where a JSON body would be shown raw to the user. Both
// title and message are HTML-escaped.
func writeHTMLErrorPage(w http.ResponseWriter, status int, title, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>%s</title>
<style>
  body { font-family: system-ui, -apple-system, sans-serif; background:#0f1115; color:#e6e6e6;
         display:flex; min-height:100vh; align-items:center; justify-content:center; margin:0; }
  .card { max-width:32rem; padding:2.5rem; background:#171a21; border:1px solid #2a2f3a;
          border-radius:12px; box-shadow:0 8px 30px rgba(0,0,0,.4); }
  h1 { font-size:1.25rem; margin:0 0 .75rem; color:#f87171; }
  p { line-height:1.5; margin:.5rem 0; color:#c4c8d0; }
  a { color:#93c5fd; }
</style>
</head>
<body>
  <div class="card">
    <h1>%s</h1>
    <p>%s</p>
    <p><a href="/login">Return to sign in</a></p>
  </div>
</body>
</html>`, html.EscapeString(title), html.EscapeString(title), html.EscapeString(message))
}

// csvSafeCell prevents CSV formula injection by prefixing cells that start
// with a formula trigger character (=, +, -, @, tab, carriage-return) with a
// single quote. Spreadsheet applications treat the value as plain text.
func csvSafeCell(s string) string {
	if len(s) > 0 && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}
