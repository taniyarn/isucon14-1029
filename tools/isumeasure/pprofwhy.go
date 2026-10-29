package main

import (
	"fmt"
	"sort"
	"strings"
)

// pprofwhy.go: when a profile is missing, say why — the agent records "error: GET <url>: http 500: <body> (0.1s)",
// a timeout, or "connection refused" per profile in run.json; no entry at all means it never tried (the run was
// shorter than -pprof-delay, or -pprof '' on that agent).

type pprofFailure struct {
	host, profile, reason string
}

func pprofFailures(hosts []*hostRun) []pprofFailure {
	var out []pprofFailure
	for _, h := range hosts {
		pm, ok := h.Run["pprof"].(map[string]any)
		if !ok {
			if num(h.Run["duration_sec"]) > 0 && strings.HasPrefix(str(h.Run["trigger"]), "POST") {
				out = append(out, pprofFailure{h.Name, "all", "no capture recorded in run.json: the run ended before -pprof-delay (20 s), or the agent runs with -pprof ''"})
			}
			continue
		}
		var names []string
		for k := range pm {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, k := range names {
			v := str(pm[k])
			if strings.HasPrefix(v, "error") {
				out = append(out, pprofFailure{h.Name, k, strings.TrimPrefix(strings.TrimPrefix(v, "error:"), " ")})
			}
		}
	}
	return out
}

// pprofHint: what the reason usually means.
func pprofHint(reason string) string {
	switch {
	case strings.Contains(reason, "already in use"):
		return "another CPU profile was running at the same time (a second agent, the panel, a person with go tool pprof) — only one at a time"
	case strings.Contains(reason, "WriteTimeout"):
		return "the pprof listener's http.Server has a WriteTimeout shorter than the profile: serve pprof on its own server without timeouts"
	case strings.Contains(reason, "http 500"):
		return "the app's pprof handler refused (usually: a CPU profile already running — two captures at once)"
	case strings.Contains(reason, "http 404"):
		return "the listener answers but has no /debug/pprof (import _ \"net/http/pprof\" missing, or a custom mux without it)"
	case strings.Contains(reason, "connection refused"):
		return "nothing listens on the pprof address in this build"
	case strings.Contains(reason, "timeout"):
		return "the app did not answer within the profile length + 15 s (CPU saturated, or a blocking handler on the pprof server)"
	}
	return ""
}

func (f pprofFailure) String() string {
	s := fmt.Sprintf("%s %s: %s", f.host, f.profile, f.reason)
	if h := pprofHint(f.reason); h != "" {
		s += " → " + h
	}
	return s
}
