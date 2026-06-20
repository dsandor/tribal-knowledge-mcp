package storage

import "testing"

func TestCoerceBooleanColumns(t *testing.T) {
	boolCols := map[string]bool{"enabled": true, "manually_assigned": true}

	t.Run("float64 one becomes true", func(t *testing.T) {
		row := map[string]any{"enabled": float64(1)}
		coerceBooleanColumns(row, boolCols)
		if row["enabled"] != true {
			t.Fatalf("got %v (%T), want true", row["enabled"], row["enabled"])
		}
	})

	t.Run("float64 zero becomes false", func(t *testing.T) {
		row := map[string]any{"enabled": float64(0)}
		coerceBooleanColumns(row, boolCols)
		if row["enabled"] != false {
			t.Fatalf("got %v (%T), want false", row["enabled"], row["enabled"])
		}
	})

	t.Run("int64 one becomes true", func(t *testing.T) {
		row := map[string]any{"enabled": int64(1)}
		coerceBooleanColumns(row, boolCols)
		if row["enabled"] != true {
			t.Fatalf("got %v (%T), want true", row["enabled"], row["enabled"])
		}
	})

	t.Run("int64 zero becomes false", func(t *testing.T) {
		row := map[string]any{"manually_assigned": int64(0)}
		coerceBooleanColumns(row, boolCols)
		if row["manually_assigned"] != false {
			t.Fatalf("got %v (%T), want false", row["manually_assigned"], row["manually_assigned"])
		}
	})

	t.Run("int one becomes true", func(t *testing.T) {
		row := map[string]any{"enabled": int(1)}
		coerceBooleanColumns(row, boolCols)
		if row["enabled"] != true {
			t.Fatalf("got %v (%T), want true", row["enabled"], row["enabled"])
		}
	})

	t.Run("missing key untouched", func(t *testing.T) {
		row := map[string]any{"other": "x"}
		coerceBooleanColumns(row, boolCols)
		if _, ok := row["enabled"]; ok {
			t.Fatalf("enabled should not have been added")
		}
		if row["other"] != "x" {
			t.Fatalf("got %v, want x", row["other"])
		}
	})

	t.Run("nil value untouched", func(t *testing.T) {
		row := map[string]any{"enabled": nil}
		coerceBooleanColumns(row, boolCols)
		if row["enabled"] != nil {
			t.Fatalf("got %v, want nil", row["enabled"])
		}
	})

	t.Run("existing bool untouched", func(t *testing.T) {
		row := map[string]any{"enabled": true}
		coerceBooleanColumns(row, boolCols)
		if row["enabled"] != true {
			t.Fatalf("got %v, want true", row["enabled"])
		}
	})

	t.Run("non-boolean column left alone", func(t *testing.T) {
		row := map[string]any{"priority": float64(5)}
		coerceBooleanColumns(row, boolCols)
		if row["priority"] != float64(5) {
			t.Fatalf("got %v (%T), want float64(5)", row["priority"], row["priority"])
		}
	})
}
