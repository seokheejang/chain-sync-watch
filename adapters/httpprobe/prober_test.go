package httpprobe_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/adapters/httpprobe"
	"github.com/seokheejang/chain-sync-watch/internal/probe"
)

func newProber() *httpprobe.Prober {
	return httpprobe.New()
}

func TestProber_200OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	target := probe.ProbeTarget{Kind: probe.TargetHTTP, URL: srv.URL, Method: "GET"}
	got := newProber().Probe(context.Background(), target, time.Second)

	require.Equal(t, http.StatusOK, got.StatusCode)
	require.Equal(t, probe.ErrorNone, got.ErrorClass)
	require.Empty(t, got.ErrorMsg)
	require.GreaterOrEqual(t, got.ElapsedMS, int64(0))
}

func TestProber_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	got := newProber().Probe(
		context.Background(),
		probe.ProbeTarget{Kind: probe.TargetHTTP, URL: srv.URL, Method: "GET"},
		time.Second,
	)
	require.Equal(t, http.StatusNotFound, got.StatusCode)
	require.Equal(t, probe.ErrorHTTP4xx, got.ErrorClass)
	require.Contains(t, got.ErrorMsg, "404")
}

func TestProber_503(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	got := newProber().Probe(
		context.Background(),
		probe.ProbeTarget{Kind: probe.TargetHTTP, URL: srv.URL, Method: "GET"},
		time.Second,
	)
	require.Equal(t, http.StatusServiceUnavailable, got.StatusCode)
	require.Equal(t, probe.ErrorHTTP5xx, got.ErrorClass)
	require.Contains(t, got.ErrorMsg, "503")
}

func TestProber_3xxIsHealthy(t *testing.T) {
	// Many indexers return 304 / 307 from cache layers. Treat as success.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()

	hc := &http.Client{
		// Disable redirect-follow so the prober sees the 3xx itself.
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
	prober := httpprobe.New(httpprobe.WithHTTPClient(hc))
	got := prober.Probe(
		context.Background(),
		probe.ProbeTarget{Kind: probe.TargetHTTP, URL: srv.URL, Method: "GET"},
		time.Second,
	)
	require.Equal(t, http.StatusNotModified, got.StatusCode)
	require.Equal(t, probe.ErrorNone, got.ErrorClass)
}

func TestProber_TimeoutClassifiedCorrectly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	got := newProber().Probe(
		context.Background(),
		probe.ProbeTarget{Kind: probe.TargetHTTP, URL: srv.URL, Method: "GET"},
		20*time.Millisecond,
	)
	require.Equal(t, 0, got.StatusCode, "no HTTP response on timeout")
	require.Equal(t, probe.ErrorTimeout, got.ErrorClass)
	require.NotEmpty(t, got.ErrorMsg)
}

func TestProber_NetworkErrorOnUnreachable(t *testing.T) {
	// Port 1 with no listener — connect refused on most platforms,
	// dial error wrapped through url.Error → ErrorNetwork.
	got := newProber().Probe(
		context.Background(),
		probe.ProbeTarget{Kind: probe.TargetHTTP, URL: "http://127.0.0.1:1/", Method: "GET"},
		2*time.Second,
	)
	require.Equal(t, 0, got.StatusCode)
	require.Equal(t, probe.ErrorNetwork, got.ErrorClass)
	require.NotEmpty(t, got.ErrorMsg)
}

func TestProber_POSTBodyAndHeaders(t *testing.T) {
	var capturedBody string
	var capturedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		capturedBody = string(buf[:n])
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	target := probe.ProbeTarget{
		Kind:    probe.TargetHTTP,
		URL:     srv.URL,
		Method:  "POST",
		Headers: map[string]string{"Authorization": "Bearer token"},
		Body:    []byte(`{"q":"ping"}`),
	}
	got := newProber().Probe(context.Background(), target, time.Second)
	require.Equal(t, http.StatusOK, got.StatusCode)
	require.Equal(t, probe.ErrorNone, got.ErrorClass)
	require.Equal(t, "Bearer token", capturedAuth)
	require.Equal(t, `{"q":"ping"}`, capturedBody)
}

func TestProber_MalformedURL(t *testing.T) {
	got := newProber().Probe(
		context.Background(),
		probe.ProbeTarget{Kind: probe.TargetHTTP, URL: "ht!tp://broken", Method: "GET"},
		time.Second,
	)
	require.Equal(t, probe.ErrorNetwork, got.ErrorClass)
	require.NotEmpty(t, got.ErrorMsg)
}

func TestProber_TruncatesLongErrMsg(t *testing.T) {
	// Force a long URL (and therefore a long error message) by using
	// a hostname long enough that the error string exceeds the cap.
	long := "http://" + strings.Repeat("a", probe.ErrMsgMaxLen) + ".invalid/"
	got := newProber().Probe(
		context.Background(),
		probe.ProbeTarget{Kind: probe.TargetHTTP, URL: long, Method: "GET"},
		2*time.Second,
	)
	require.Equal(t, probe.ErrorNetwork, got.ErrorClass)
	require.LessOrEqual(t, len(got.ErrorMsg), probe.ErrMsgMaxLen)
}

func TestProber_DefaultMethodIsGET(t *testing.T) {
	var seenMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	got := newProber().Probe(
		context.Background(),
		probe.ProbeTarget{Kind: probe.TargetHTTP, URL: srv.URL, Method: ""},
		time.Second,
	)
	require.Equal(t, http.StatusOK, got.StatusCode)
	require.Equal(t, http.MethodGet, seenMethod)
}

func TestProber_RespectsCallerContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the probe starts

	got := newProber().Probe(
		ctx,
		probe.ProbeTarget{Kind: probe.TargetHTTP, URL: srv.URL, Method: "GET"},
		time.Second,
	)
	// A pre-cancelled context surfaces as the caller's deadline
	// (treated as ErrorTimeout) — same dashboard signal as a server
	// that didn't respond in time.
	require.Equal(t, probe.ErrorTimeout, got.ErrorClass)
}
