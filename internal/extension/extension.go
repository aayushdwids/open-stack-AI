// Package extension is the seam between the Apache-2.0 engine and the proprietary
// team-observability tier. The engine defines the interface and a registry; the enterprise
// build registers an implementation via init() under the `enterprise` build tag. Default
// builds register nothing, so they link no proprietary code and expose only free features.
package extension

import "github.com/faraday-stack/faraday/internal/store"

// TeamObserver provides team-scale (paid) observability over the span/eval store.
type TeamObserver interface {
	// Summary aggregates across the whole store (cross-user/run) — a team-tier feature.
	Summary(st *store.Store) (map[string]any, error)
}

var teamObserver TeamObserver

// RegisterTeamObserver installs the team observer (called by the enterprise build).
func RegisterTeamObserver(o TeamObserver) { teamObserver = o }

// TeamObserverImpl returns the registered observer, or nil if not built/enabled.
func TeamObserverImpl() TeamObserver { return teamObserver }
