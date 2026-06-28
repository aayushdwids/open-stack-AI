//go:build enterprise

// Package teamobs is the PROPRIETARY team-observability tier (commercial license — see
// enterprise/LICENSE). It is compiled only with `-tags enterprise`. It aggregates the span
// and eval store across users/runs — a paid, team-scale capability — and is gated by an
// offline-verified team license.
package teamobs

import (
	"errors"
	"os"

	"github.com/faraday-stack/faraday/internal/extension"
	"github.com/faraday-stack/faraday/internal/license"
	"github.com/faraday-stack/faraday/internal/store"
)

func init() {
	extension.RegisterTeamObserver(&observer{})
}

type observer struct{}

// Summary aggregates eval runs across the whole store into a team-wide view. Requires a
// valid, offline-verified team license; otherwise it refuses (the feature is paid).
func (o *observer) Summary(st *store.Store) (map[string]any, error) {
	claims, paid := license.Active(os.Getenv("FARADAY_LICENSE"))
	if !paid || !claims.HasFeature("team-aggregation") {
		return nil, errors.New("team aggregation requires a valid team license (offline)")
	}
	runs, err := st.ListEvalRuns(0)
	if err != nil {
		return nil, err
	}
	bySuite := map[string]map[string]any{}
	var totalPass, total int
	for _, r := range runs {
		s := bySuite[r.Suite]
		if s == nil {
			s = map[string]any{"runs": 0, "passed": 0}
			bySuite[r.Suite] = s
		}
		s["runs"] = s["runs"].(int) + 1
		if r.Passed {
			s["passed"] = s["passed"].(int) + 1
			totalPass++
		}
		total++
	}
	return map[string]any{
		"licensee":      claims.Licensee,
		"tier":          claims.Tier,
		"total_runs":    total,
		"passed_runs":   totalPass,
		"by_suite":      bySuite,
		"aggregated":    true,
	}, nil
}
