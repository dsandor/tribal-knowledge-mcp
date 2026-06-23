package storage

import "testing"

func TestQuoteIdents(t *testing.T) {
	if got := quoteIdents([]string{"id", "team_id"}); got != `"id", "team_id"` {
		t.Errorf("quoteIdents = %q", got)
	}
	if got := quoteIdents([]string{"a"}); got != `"a"` {
		t.Errorf("single ident = %q", got)
	}
	// embedded quote is escaped by doubling
	if got := quoteIdents([]string{`we"ird`}); got != `"we""ird"` {
		t.Errorf("escaping = %q", got)
	}
}

func TestCoerceNotNullColumns(t *testing.T) {
	notNull := map[string]string{
		"team_id":      "text",
		"user_id":      "character varying",
		"version":      "integer",
		"rating":       "double precision",
		"enabled":      "boolean",
		"last_used_at": "timestamp with time zone", // nullable-in-practice; unhandled type
	}

	t.Run("nil values get typed zeros", func(t *testing.T) {
		row := map[string]any{
			"team_id": nil,
			"user_id": nil,
			"version": nil,
			"rating":  nil,
			"enabled": nil,
		}
		coerceNotNullColumns(row, notNull)
		if row["team_id"] != "" {
			t.Errorf("team_id = %v, want \"\"", row["team_id"])
		}
		if row["user_id"] != "" {
			t.Errorf("user_id = %v, want \"\"", row["user_id"])
		}
		if row["version"] != 0 {
			t.Errorf("version = %v, want 0", row["version"])
		}
		if row["rating"] != 0 {
			t.Errorf("rating = %v, want 0", row["rating"])
		}
		if row["enabled"] != false {
			t.Errorf("enabled = %v, want false", row["enabled"])
		}
	})

	t.Run("missing NOT NULL key is filled", func(t *testing.T) {
		row := map[string]any{} // team_id absent entirely
		coerceNotNullColumns(row, notNull)
		if row["team_id"] != "" {
			t.Errorf("team_id = %v, want \"\" for missing key", row["team_id"])
		}
	})

	t.Run("concrete values are preserved", func(t *testing.T) {
		row := map[string]any{
			"team_id": "team-123",
			"version": int64(5),
			"enabled": true,
		}
		coerceNotNullColumns(row, notNull)
		if row["team_id"] != "team-123" {
			t.Errorf("team_id overwritten: %v", row["team_id"])
		}
		if row["version"] != int64(5) {
			t.Errorf("version overwritten: %v", row["version"])
		}
		if row["enabled"] != true {
			t.Errorf("enabled overwritten: %v", row["enabled"])
		}
	})

	t.Run("unhandled type with nil is left untouched", func(t *testing.T) {
		row := map[string]any{"last_used_at": nil}
		coerceNotNullColumns(row, notNull)
		if row["last_used_at"] != nil {
			t.Errorf("last_used_at should be left nil for unhandled type, got %v", row["last_used_at"])
		}
	})
}
