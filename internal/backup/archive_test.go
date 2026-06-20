package backup

import "testing"

func TestCoveredTablesOrderingAndExclusions(t *testing.T) {
	got := CoveredTables()
	if len(got) == 0 {
		t.Fatal("CoveredTables is empty")
	}
	// teams must precede users and api_keys (FK parents first)
	idx := map[string]int{}
	for i, name := range got {
		idx[name] = i
	}
	for _, child := range []string{"users", "api_keys"} {
		if idx["teams"] > idx[child] {
			t.Errorf("teams must come before %s", child)
		}
	}
	if idx["entries"] > idx["clusters"] {
		t.Error("entries must come before clusters")
	}
	for _, excluded := range []string{"sessions", "vec_entries", "entry_embeddings", "embeddings"} {
		if _, ok := idx[excluded]; ok {
			t.Errorf("%s must not be in CoveredTables", excluded)
		}
	}
}

func TestFormatVersionConst(t *testing.T) {
	if FormatVersion != 1 {
		t.Errorf("FormatVersion = %d, want 1", FormatVersion)
	}
}
