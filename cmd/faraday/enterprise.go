//go:build enterprise

package main

// Blank-import the proprietary team-observability tier so its init() registers the team
// observer. Compiled ONLY with `-tags enterprise`; default builds link none of it.
import _ "github.com/faraday-stack/faraday/enterprise/teamobs"
