package mcp

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/szymonrychu/tatara-cli/internal/client"
)

// memoryDegradedEnv is set to "true" by tatara-operator when it spawns an agent
// pod whose tatara-memory backend is unhealthy. The operator deliberately still
// injects TATARA_MEMORY_URL on that path, and deliberately runs the agent
// anyway with prompt guidance to proceed on reduced recall (tatara-operator
// #470). This flag is how the tool layer stops contradicting that guidance with
// a bare transport error.
const memoryDegradedEnv = "TATARA_MEMORY_DEGRADED"

// memoryDisabledEnv is injected alongside memoryDegradedEnv and answers a
// different question. DEGRADED answers "will the recall tools fail?" - yes,
// either way. This one answers "is that a fault?", and when it is true the
// answer is no: the project is configured without memory on purpose, so there
// is nothing to report (tatara-operator#523).
const memoryDisabledEnv = "TATARA_MEMORY_DISABLED"

// guidanceHead opens both full forms. The "MEMORY_DEGRADED:" prefix is
// load-bearing: skills and prompts match on that literal, so it stays byte
// stable whichever tail follows it.
const guidanceHead = "MEMORY_DEGRADED: the tatara-memory subsystem is unavailable (%s). " +
	"This is a known platform condition, not a mistake in your request. " +
	"Proceed WITHOUT recall - use Serena/LSP, git, and direct file reads instead. "

// guidanceTail closes both full forms.
const guidanceTail = "State in your outcome that memory recall was unavailable, and complete your work."

// fullGuidance is the mid-turn form: the operator said healthy at spawn and the
// backend died afterwards, so nothing upstream knows yet and this process is
// the only witness. It asks for exactly one report_internal_issue - description
// is included because that argument is required and a guidance line that fails
// validation is worse than none.
const fullGuidance = guidanceHead +
	"Call report_internal_issue(category=\"tool_error\", offending_tool=%q, " +
	"description=\"tatara-memory unavailable\") ONCE this turn. " + guidanceTail

// knownGuidance is the spawn-time form: the verdict came from the operator,
// which has already told the agent at turn 0 - in
// promptguidance.MemoryDegradedGuidance and MemoryDisabledGuidance alike - not
// to report this condition. Asking for a report here contradicts the prompt and
// manufactures one duplicate platform-problem alert per turn
// (tatara-operator#523).
const knownGuidance = guidanceHead +
	"Do NOT call report_internal_issue for this: the condition is already tracked upstream, " +
	"so a report would only duplicate an alert that already exists. " + guidanceTail

// shortGuidance answers every later occurrence. One outage hits every memory
// tool the agent tries; repeating the full paragraph each time would flood the
// transcript and drown the work the agent is supposed to carry on with.
const shortGuidance = "MEMORY_DEGRADED (see earlier): tatara-memory is still unavailable (%s). " +
	"Proceed without recall; do not report it again."

// memoryState tracks whether tatara-memory is usable for the life of this
// process (one process = one agent turn). It has two entry points: the
// operator's spawn-time verdict, and a latch set by the first backend failure
// we see ourselves, which catches an outage that starts mid-turn.
type memoryState struct {
	mu        sync.Mutex
	reason    string // non-empty once memory is known unusable
	upstream  bool   // the reason is the operator's spawn-time verdict, not our own latch
	announced bool   // full guidance already emitted
}

// newMemoryState reads the spawn-time verdict. configured is false when no
// memory base URL resolved at all (TATARA_MEMORY_URL set but empty), in which
// case there is no client to call and the pod is degraded from the start.
//
// Every arm here is a condition the operator decided and already stated at turn
// 0, so all three set upstream: the agent must be told to proceed, not to
// report. The disabled arm is checked before the degraded one because both are
// true on a project configured without memory, and only the disabled wording is
// true - that project was never "flagged unhealthy".
func newMemoryState(configured bool) *memoryState {
	s := &memoryState{}
	switch {
	case !configured:
		s.reason = "no memory backend is configured for this pod: TATARA_MEMORY_URL is set but empty"
	case os.Getenv(memoryDisabledEnv) == "true":
		s.reason = "this project is configured without memory (" + memoryDisabledEnv + "=true)"
	case os.Getenv(memoryDegradedEnv) == "true":
		s.reason = "the platform flagged it unhealthy when this pod started (" + memoryDegradedEnv + "=true)"
	}
	s.upstream = s.reason != ""
	return s
}

// degraded returns the reason memory is unusable, if it is.
func (s *memoryState) degraded() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reason, s.reason != ""
}

// latch records the first backend failure and returns the reason to report.
// Later failures reuse the first one: it is the outage the agent should hear
// about, and it is the one the short form refers back to.
func (s *memoryState) latch(err error) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reason == "" {
		s.reason = err.Error()
	}
	return s.reason
}

// report renders the agent-facing result for tool: a full guidance on the first
// occurrence, the short form after that. Which full form depends on who knows:
// a spawn-time verdict is already alerted on upstream and must not be reported
// again, a mid-turn latch is this process's own discovery and must be.
func (s *memoryState) report(tool, reason string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.announced {
		return fmt.Sprintf(shortGuidance, reason)
	}
	s.announced = true
	if s.upstream {
		return fmt.Sprintf(knownGuidance, reason)
	}
	return fmt.Sprintf(fullGuidance, reason, tool)
}

// memoryBackendDown reports whether an Invoke failure against the memory
// backend is the backend's own - a transport failure (nothing answered) or a
// 5xx - rather than something the agent caused.
//
// 4xx is deliberately excluded. A 400/404/409 means the request was wrong, not
// that memory is down; latching on one would hide a perfectly healthy backend
// behind a MEMORY_DEGRADED banner for the rest of the turn and tell the agent
// to give up on recall it could still have. 401/403 are excluded for the same
// reason: they are an auth problem, and calling them a memory outage would send
// the agent chasing the wrong subsystem.
func memoryBackendDown(err error) bool {
	var se *statusError
	if errors.As(err, &se) {
		return se.code >= 500
	}
	var te *client.TransportError
	return errors.As(err, &te)
}
