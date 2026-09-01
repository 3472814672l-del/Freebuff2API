package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"
)

type UpstreamClient struct {
	baseURL    string
	httpClient *http.Client
	userAgent  string
}

// utlsConn wraps a utls.UConn to implement the ConnectionState method
// that Go's http2 transport needs to detect ALPN negotiation.
type utlsConn struct {
	*utls.UConn
}

// newUTLSTransport creates an HTTP/2 transport that uses uTLS for TLS,
// presenting a Chrome-like TLS fingerprint to bypass server-side CLI detection.
func newUTLSTransport(proxyURL *url.URL, timeout time.Duration) http.RoundTripper {
	// Dialer for TCP connections.
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	// Custom dial function that returns a uTLS connection with Chrome fingerprint.
	dialTLS := func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("split address: %w", err)
		}

		tcpConn, err := dialer.DialContext(ctx, network, addr)
		if err != nil {
			return nil, fmt.Errorf("dial TCP: %w", err)
		}

		tlsConfig := &utls.Config{
			ServerName: host,
			MinVersion: tls.VersionTLS12,
			NextProtos: []string{"h2", "http/1.1"}, // Offer both h2 and http/1.1.
		}

		uConn := utls.UClient(tcpConn, tlsConfig, utls.HelloChrome_120)

		// Build handshake state to modify ALPN if needed.
		if err := uConn.BuildHandshakeState(); err != nil {
			tcpConn.Close()
			return nil, fmt.Errorf("build handshake state: %w", err)
		}

		if err := uConn.HandshakeContext(ctx); err != nil {
			tcpConn.Close()
			return nil, fmt.Errorf("TLS handshake: %w", err)
		}

		return uConn, nil
	}

	// Create an http2.Transport with our custom dialer.
	// This gives us HTTP/2 framing over uTLS connections.
	h2transport := &http2.Transport{
		DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			return dialTLS(ctx, network, addr)
		},
		AllowHTTP: false,
	}

	return h2transport
}

func NewUpstreamClient(cfg Config) *UpstreamClient {
	var proxyURL *url.URL
	if cfg.HTTPProxy != "" {
		if parsed, err := url.Parse(cfg.HTTPProxy); err == nil {
			proxyURL = parsed
		}
	}

	transport := newUTLSTransport(proxyURL, cfg.RequestTimeout)

	return &UpstreamClient{
		baseURL: cfg.UpstreamBaseURL,
		httpClient: &http.Client{
			Timeout:   cfg.RequestTimeout,
			Transport: transport,
		},
		userAgent: cfg.UserAgent,
	}
}

func (c *UpstreamClient) StartRun(ctx context.Context, authToken, agentID string) (string, error) {
	payload := map[string]any{
		"action":  "START",
		"agentId": agentID,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal start run request: %w", err)
	}

	resp, err := c.doJSON(ctx, authToken, "/api/v1/agent-runs", body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read start run response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("start run failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}

	var parsed struct {
		RunID string `json:"runId"`
	}
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		return "", fmt.Errorf("decode start run response: %w", err)
	}
	if strings.TrimSpace(parsed.RunID) == "" {
		return "", fmt.Errorf("start run response missing runId: %s", strings.TrimSpace(string(responseBody)))
	}

	return parsed.RunID, nil
}

func (c *UpstreamClient) FinishRun(ctx context.Context, authToken, runID string, totalSteps int) error {
	payload := map[string]any{
		"action":        "FINISH",
		"runId":         runID,
		"status":        "completed",
		"totalSteps":    totalSteps,
		"directCredits": 0,
		"totalCredits":  0,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal finish run request: %w", err)
	}

	resp, err := c.doJSON(ctx, authToken, "/api/v1/agent-runs", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read finish run response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("finish run failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	return nil
}

func (c *UpstreamClient) ChatCompletions(ctx context.Context, authToken string, body []byte) (*http.Response, []byte, error) {
	resp, err := c.doJSON(ctx, authToken, "/api/v1/chat/completions", body)
	if err != nil {
		return nil, nil, err
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp, nil, nil
	}

	responseBody, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	if readErr != nil {
		return nil, nil, fmt.Errorf("read upstream error response: %w", readErr)
	}
	return resp, responseBody, nil
}

func (c *UpstreamClient) doJSON(ctx context.Context, authToken, path string, body []byte) (*http.Response, error) {
	requestURL, err := url.JoinPath(c.baseURL, path)
	if err != nil {
		return nil, fmt.Errorf("build upstream url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+authToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Connection", "keep-alive")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send upstream request: %w", err)
	}
	return resp, nil
}

func retryAfterDuration(headerValue string) time.Duration {
	headerValue = strings.TrimSpace(headerValue)
	if headerValue == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(headerValue); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return 0
}
