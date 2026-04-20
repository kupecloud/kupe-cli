package cli

import (
	"fmt"
	"time"

	"github.com/kupecloud/kupe-cli/internal/client"
)

// verboseTrace returns a client.TraceFunc that emits one stderr line per
// HTTP round-trip in a format like:
//
//	[verbose] GET  /api/v1/tenants/acme/clusters 200 45ms (request-id: 7a3b…)
//
// Honours the IOStreams' ErrOut so tests can capture it. Tokens are never
// passed to the trace func by the client, so nothing sensitive ends up on
// the wire.
func verboseTrace(io *IOStreams) client.TraceFunc {
	return func(method, path string, status int, duration time.Duration, requestID string) {
		id := ""
		if requestID != "" {
			id = fmt.Sprintf(" (request-id: %s)", requestID)
		}
		fmt.Fprintf(io.ErrOut, "[verbose] %-6s %s %d %s%s\n",
			method, path, status, duration.Round(time.Millisecond), id)
	}
}
