package web

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteHTMLErrorPage(t *testing.T) {
	rec := httptest.NewRecorder()
	writeHTMLErrorPage(rec, 400, "Single sign-on is not configured correctly", "No email was supplied by your identity provider.")

	if rec.Code != 400 {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Single sign-on is not configured correctly") {
		t.Error("body missing title")
	}
	if !strings.Contains(body, "No email was supplied by your identity provider.") {
		t.Error("body missing detail message")
	}
	if !strings.Contains(strings.ToLower(body), "<!doctype html") {
		t.Error("body is not an HTML document")
	}
}

func TestWriteHTMLErrorPageEscapesInput(t *testing.T) {
	rec := httptest.NewRecorder()
	writeHTMLErrorPage(rec, 400, "Title", "<script>alert(1)</script>")
	if strings.Contains(rec.Body.String(), "<script>alert(1)</script>") {
		t.Error("detail message was not HTML-escaped")
	}
}
