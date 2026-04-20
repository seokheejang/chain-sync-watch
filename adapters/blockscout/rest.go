package blockscout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/seokheejang/chain-sync-watch/internal/source"
)

// getJSON issues a REST v2 GET and decodes the body into out.
// Treats 404 as ErrNotFound; 429 as rate-limit; 5xx as unavailable.
func (a *Adapter) getJSON(ctx context.Context, path string, out any) error {
	u := a.base + "/api/v2" + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return fmt.Errorf("blockscout: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := a.hc.Do(ctx, req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return fmt.Errorf("%w: %v", source.ErrSourceUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return source.ErrNotFound
	case resp.StatusCode == http.StatusTooManyRequests:
		return source.ErrRateLimited
	case resp.StatusCode >= 500:
		return fmt.Errorf("%w: upstream status %d", source.ErrSourceUnavailable, resp.StatusCode)
	case resp.StatusCode >= 400:
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%w: http %d: %s", source.ErrInvalidResponse, resp.StatusCode, preview(body))
	}

	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%w: decode: %v", source.ErrInvalidResponse, err)
	}
	return nil
}

func preview(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}
