package web

import (
	"encoding/json"
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

// csvSafeCell prevents CSV formula injection by prefixing cells that start
// with a formula trigger character (=, +, -, @, tab, carriage-return) with a
// single quote. Spreadsheet applications treat the value as plain text.
func csvSafeCell(s string) string {
	if len(s) > 0 && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}
