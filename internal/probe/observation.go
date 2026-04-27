package probe

import (
	"errors"
	"fmt"
	"time"
)

// ErrorClass classifies how a single Observation failed (or didn't).
// Persistence stores it as a small integer so the wire format and the
// table column are both compact, and the constants are stable across
// versions — never reorder.
type ErrorClass int

const (
	// ErrorNone — the call succeeded; no error semantics attached.
	ErrorNone ErrorClass = iota
	// ErrorNetwork — DNS, TCP, TLS handshake. The HTTP response
	// never arrived.
	ErrorNetwork
	// ErrorTimeout — the call exceeded its deadline. Distinct from
	// ErrorNetwork because some triage paths only care about
	// deadline-exceeded as a load signal.
	ErrorTimeout
	// ErrorHTTP4xx — the server responded with 4xx. Probe author's
	// problem most of the time (auth, missing route, malformed body).
	ErrorHTTP4xx
	// ErrorHTTP5xx — server-side fault. The most actionable signal
	// for a service-health probe.
	ErrorHTTP5xx
	// ErrorProtocol — transport succeeded but the payload reported a
	// domain-level error: JSON-RPC error code, GraphQL `errors[]`,
	// non-200 SOAP envelope, etc.
	ErrorProtocol
)

// String returns a stable, lowercase identifier suitable for API
// output and persistence. Mirrors the constant ordering above.
func (e ErrorClass) String() string {
	switch e {
	case ErrorNone:
		return "none"
	case ErrorNetwork:
		return "network"
	case ErrorTimeout:
		return "timeout"
	case ErrorHTTP4xx:
		return "http_4xx"
	case ErrorHTTP5xx:
		return "http_5xx"
	case ErrorProtocol:
		return "protocol"
	default:
		return fmt.Sprintf("unknown(%d)", int(e))
	}
}

// IsError reports whether the class signals a failure for the
// purposes of error-rate aggregation. ErrorNone is the only
// non-failure class; anything else counts.
func (e ErrorClass) IsError() bool { return e != ErrorNone }

// ParseErrorClass is the inverse of String. Persistence and HTTP API
// layers use it to round-trip the textual form. Returns an error on
// unknown values rather than silently mapping to ErrorNone, which
// would mask data corruption.
func ParseErrorClass(s string) (ErrorClass, error) {
	switch s {
	case "none":
		return ErrorNone, nil
	case "network":
		return ErrorNetwork, nil
	case "timeout":
		return ErrorTimeout, nil
	case "http_4xx":
		return ErrorHTTP4xx, nil
	case "http_5xx":
		return ErrorHTTP5xx, nil
	case "protocol":
		return ErrorProtocol, nil
	default:
		return ErrorNone, fmt.Errorf("unknown error class %q", s)
	}
}

// Observation is one immutable measurement of a Probe call. The
// schema is deliberately narrow: anything that requires storing the
// response body or the request payload belongs in a separate audit
// log, not here, where the per-row volume budget is tight.
//
// ErrorMsg is truncated by the producer (HTTPProber) to keep the
// row size bounded; the domain doesn't enforce a length but
// callers should treat ErrMsgMaxLen as the ceiling.
type Observation struct {
	ProbeID    ProbeID
	At         time.Time
	ElapsedMS  int64
	StatusCode int
	ErrorClass ErrorClass
	ErrorMsg   string
}

// ErrMsgMaxLen is the recommended truncation length for Observation.ErrorMsg.
// The persistence layer enforces an absolute cap at the column level;
// adapters should pre-truncate so that field never reaches the column
// trimmer.
const ErrMsgMaxLen = 512

// NewObservation constructs an Observation after validating the
// invariants that downstream consumers rely on:
//
//   - ProbeID non-empty
//   - At non-zero
//   - ElapsedMS non-negative (a probe that responded faster than its
//     start time is always a clock bug)
//   - StatusCode plausible (0 means "no HTTP response", e.g. for
//     ErrorNetwork; otherwise it must be in the [100, 599] range)
//   - ErrorClass mirrors StatusCode where applicable (5xx ↔ ErrorHTTP5xx)
func NewObservation(
	probeID ProbeID,
	at time.Time,
	elapsedMS int64,
	statusCode int,
	class ErrorClass,
	errMsg string,
) (Observation, error) {
	if probeID == "" {
		return Observation{}, errors.New("observation: probe id is empty")
	}
	if at.IsZero() {
		return Observation{}, errors.New("observation: timestamp is zero")
	}
	if elapsedMS < 0 {
		return Observation{}, fmt.Errorf("observation: elapsed_ms negative: %d", elapsedMS)
	}
	if statusCode != 0 && (statusCode < 100 || statusCode > 599) {
		return Observation{}, fmt.Errorf("observation: status_code out of range: %d", statusCode)
	}
	// Status/class consistency check: the HTTP-class enum values
	// must agree with the response code. ErrorProtocol is allowed to
	// pair with a 200 (the transport succeeded but the body says no).
	switch class {
	case ErrorHTTP4xx:
		if statusCode < 400 || statusCode >= 500 {
			return Observation{}, fmt.Errorf("observation: http_4xx with status_code %d", statusCode)
		}
	case ErrorHTTP5xx:
		if statusCode < 500 || statusCode >= 600 {
			return Observation{}, fmt.Errorf("observation: http_5xx with status_code %d", statusCode)
		}
	case ErrorNetwork, ErrorTimeout:
		if statusCode != 0 {
			return Observation{}, fmt.Errorf("observation: %s with non-zero status_code %d", class, statusCode)
		}
	}
	if len(errMsg) > ErrMsgMaxLen {
		errMsg = errMsg[:ErrMsgMaxLen]
	}
	return Observation{
		ProbeID:    probeID,
		At:         at,
		ElapsedMS:  elapsedMS,
		StatusCode: statusCode,
		ErrorClass: class,
		ErrorMsg:   errMsg,
	}, nil
}
