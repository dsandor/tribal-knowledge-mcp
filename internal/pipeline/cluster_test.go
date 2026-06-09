package pipeline

import (
	"math"
	"sort"
	"testing"
)

func TestCosineSim_Identical(t *testing.T) {
	a := []float32{1, 0, 0, 0}
	if got := cosineSim(a, a); math.Abs(got-1.0) > 1e-6 {
		t.Errorf("cosineSim(identical) = %v, want 1.0", got)
	}
}

func TestCosineSim_Orthogonal(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{0, 1}
	if got := cosineSim(a, b); math.Abs(got) > 1e-6 {
		t.Errorf("cosineSim(orthogonal) = %v, want 0.0", got)
	}
}

func TestCosineSim_ZeroVector(t *testing.T) {
	a := []float32{0, 0}
	b := []float32{1, 0}
	if got := cosineSim(a, b); got != 0 {
		t.Errorf("cosineSim(zero) = %v, want 0.0", got)
	}
}

func TestCluster_GroupsSimilar(t *testing.T) {
	embs := map[string][]float32{
		"a": {1, 0, 0, 0},
		"b": {0.99, 0.14, 0, 0}, // very similar to a (cos sim ≈ 0.99)
		"c": {0, 0, 0, 1},        // orthogonal
	}
	domains := map[string]string{"a": "finance", "b": "finance", "c": "legal"}

	clusters := Cluster(embs, domains, 0.9)

	if len(clusters) != 1 {
		t.Fatalf("expected 1 cluster, got %d: %+v", len(clusters), clusters)
	}
	ids := make([]string, len(clusters[0].EntryIDs))
	copy(ids, clusters[0].EntryIDs)
	sort.Strings(ids)
	if len(ids) != 2 || ids[0] != "a" || ids[1] != "b" {
		t.Errorf("cluster entries = %v, want [a b]", ids)
	}
	if clusters[0].Domain != "finance" {
		t.Errorf("cluster domain = %q, want finance", clusters[0].Domain)
	}
}

func TestCluster_NoClusters_AllDissimilar(t *testing.T) {
	embs := map[string][]float32{
		"a": {1, 0},
		"b": {0, 1},
	}
	clusters := Cluster(embs, map[string]string{}, 0.9)
	if len(clusters) != 0 {
		t.Errorf("expected 0 clusters, got %d", len(clusters))
	}
}

func TestCluster_SingleEntry_NotClustered(t *testing.T) {
	embs := map[string][]float32{
		"a": {1, 0},
	}
	clusters := Cluster(embs, map[string]string{}, 0.9)
	if len(clusters) != 0 {
		t.Errorf("single entry should not produce a cluster, got %d", len(clusters))
	}
}

func TestCluster_EmptyEmbeddings(t *testing.T) {
	clusters := Cluster(map[string][]float32{}, map[string]string{}, 0.9)
	if len(clusters) != 0 {
		t.Errorf("empty input should produce 0 clusters, got %d", len(clusters))
	}
}

func TestMajorityDomain_Tie(t *testing.T) {
	// Tie between "a" and "b" — lexicographically "a" wins
	ids := []string{"x", "y"}
	domains := map[string]string{"x": "b", "y": "a"}
	got := majorityDomain(ids, domains)
	if got != "a" {
		t.Errorf("tie should pick lexicographically smallest: got %q, want %q", got, "a")
	}
}
