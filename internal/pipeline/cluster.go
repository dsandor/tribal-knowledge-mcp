package pipeline

import "math"

// ClusterCandidate is a group of semantically related entry IDs.
type ClusterCandidate struct {
	EntryIDs []string
	Domain   string
}

// Cluster groups entries whose embeddings have cosine similarity >= threshold.
// embeddings maps entry ID to its float32 vector.
// domains maps entry ID to its domain string (used to pick the cluster's majority domain).
// Only groups of 2+ entries are returned.
func Cluster(embeddings map[string][]float32, domains map[string]string, threshold float64) []ClusterCandidate {
	ids := make([]string, 0, len(embeddings))
	for id := range embeddings {
		ids = append(ids, id)
	}

	assigned := make(map[string]bool, len(ids))
	var candidates []ClusterCandidate

	for _, seed := range ids {
		if assigned[seed] {
			continue
		}
		group := []string{seed}
		assigned[seed] = true

		for _, other := range ids {
			if assigned[other] {
				continue
			}
			if cosineSim(embeddings[seed], embeddings[other]) >= threshold {
				group = append(group, other)
				assigned[other] = true
			}
		}

		if len(group) >= 2 {
			candidates = append(candidates, ClusterCandidate{
				EntryIDs: group,
				Domain:   majorityDomain(group, domains),
			})
		}
	}
	return candidates
}

func cosineSim(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		ai, bi := float64(a[i]), float64(b[i])
		dot += ai * bi
		normA += ai * ai
		normB += bi * bi
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

func majorityDomain(ids []string, domains map[string]string) string {
	counts := make(map[string]int)
	for _, id := range ids {
		counts[domains[id]]++
	}
	var best string
	var bestCount int
	for d, n := range counts {
		if n > bestCount || (n == bestCount && d < best) {
			best = d
			bestCount = n
		}
	}
	return best
}
