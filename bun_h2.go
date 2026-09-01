package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
)

// Bun-compatible HTTP/2 constants (extracted from Bun source code)
// Source: src/http/H2Client.rs, src/http/h2_client/encode.rs
const (
	bunInitialWindowSize = 1 << 24     // 16777216 (16MB) — LOCAL_INITIAL_WINDOW_SIZE
	bunMaxHeaderListSize = 256 * 1024 // 262144 (256KB) — LOCAL_MAX_HEADER_LIST_SIZE
	bunDefaultWindowSize = 65535       // spec default — DEFAULT_WINDOW_SIZE
	bunWindowUpdateDelta = bunInitialWindowSize - bunDefaultWindowSize // 16711681
)

// h2Stream tracks the state of a single HTTP/2 stream.
type h2Stream struct {
	id         uint32
	headersCh  chan http.Header
	bodyCh     chan []byte
	errCh      chan error
	endHeaders bool
	endStream  bool
}

// bunH2Conn is a minimal HTTP/2 client that sends Bun-compatible frames.
// It handles one request at a time (no multiplexing) which is sufficient
// for the Freebuff2API proxy use case.
type bunH2Conn struct {
	conn     net.Conn
	framer   *http2.Framer
	encoder  *hpack.Encoder
	decoder  *hpack.Decoder
	mu       sync.Mutex
	nextID   uint32
	closed   bool
	streams  map[uint32]*h2Stream
	readDone chan struct{}
}

func dialBunH2(ctx context.Context, host string, port int) (*bunH2Conn, error) {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}

	tcpConn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial TCP: %w", err)
	}

	tlsConfig := &utls.Config{
		ServerName: host,
		MinVersion: tls.VersionTLS12,
		NextProtos: []string{"h2", "http/1.1"},
	}

	uConn := utls.UClient(tcpConn, tlsConfig, utls.HelloChrome_120)
	if err := uConn.BuildHandshakeState(); err != nil {
		tcpConn.Close()
		return nil, fmt.Errorf("build handshake state: %w", err)
	}
	if err := uConn.HandshakeContext(ctx); err != nil {
		tcpConn.Close()
		return nil, fmt.Errorf("TLS handshake: %w", err)
	}

	state := uConn.ConnectionState()
	if state.NegotiatedProtocol != "h2" {
		tcpConn.Close()
		return nil, fmt.Errorf("server did not negotiate HTTP/2 (got ALPN: %s)", state.NegotiatedProtocol)
	}

	c := &bunH2Conn{
		conn:     uConn,
		framer:   http2.NewFramer(uConn, uConn),
		encoder:  hpack.NewEncoder(new(bytes.Buffer)),
		decoder:  hpack.NewDecoder(64*1024, func(hpack.HeaderField) {}),
		nextID:   1,
		streams:  make(map[uint32]*h2Stream),
		readDone: make(chan struct{}),
	}

	if err := c.sendPreface(); err != nil {
		uConn.Close()
		return nil, fmt.Errorf("send preface: %w", err)
	}

	go c.readLoop()

	return c, nil
}

// sendPreface sends the Bun-compatible HTTP/2 connection preface:
// 1. Client preface magic ("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n")
// 2. SETTINGS frame with exactly 3 settings (matching Bun's encode.rs)
// 3. WINDOW_UPDATE on stream 0 (matching Bun's encode.rs)
func (c *bunH2Conn) sendPreface() error {
	// 1. Client preface
	if _, err := c.conn.Write([]byte(http2.ClientPreface)); err != nil {
		return fmt.Errorf("write preface: %w", err)
	}

	// 2. SETTINGS frame (exact match to Bun's write_preface in encode.rs)
	settings := []http2.Setting{
		{ID: http2.SettingEnablePush, Val: 0},
		{ID: http2.SettingInitialWindowSize, Val: bunInitialWindowSize},
		{ID: http2.SettingMaxHeaderListSize, Val: bunMaxHeaderListSize},
	}
	if err := c.framer.WriteSettings(settings...); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}

	// 3. Connection-level WINDOW_UPDATE (Bun opens window to match per-stream window)
	if err := c.framer.WriteWindowUpdate(0, bunWindowUpdateDelta); err != nil {
		return fmt.Errorf("write window update: %w", err)
	}

	return nil
}

// readLoop processes incoming HTTP/2 frames in a dedicated goroutine.
func (c *bunH2Conn) readLoop() {
	defer close(c.readDone)
	for {
		frame, err := c.framer.ReadFrame()
		if err != nil {
			// Notify all active streams of the error
			c.mu.Lock()
			for _, s := range c.streams {
				select {
				case s.errCh <- fmt.Errorf("connection closed: %w", err):
				default:
				}
			}
			c.mu.Unlock()
			return
		}

		switch f := frame.(type) {
		case *http2.SettingsFrame:
			if !f.IsAck() {
				c.framer.WriteSettingsAck()
			}

		case *http2.PingFrame:
			if !f.IsAck() {
				c.framer.WritePing(true, f.Data)
			}

		case *http2.WindowUpdateFrame:
			// Flow control update — no action needed for our simple client

		case *http2.GoAwayFrame:
			c.mu.Lock()
			for _, s := range c.streams {
				select {
				case s.errCh <- fmt.Errorf("server sent GOAWAY"):
				default:
				}
			}
			c.mu.Unlock()
			return

		case *http2.RSTStreamFrame:
			c.mu.Lock()
			if s, ok := c.streams[f.StreamID]; ok {
				select {
				case s.errCh <- fmt.Errorf("stream %d reset with code %d", f.StreamID, f.ErrCode):
				default:
				}
			}
			c.mu.Unlock()

		case *http2.HeadersFrame:
			c.mu.Lock()
			s, ok := c.streams[f.StreamID]
			c.mu.Unlock()
			if !ok {
				continue
			}

			// Decode HPACK headers
			decoded, err := c.decoder.DecodeFull(f.HeaderBlockFragment())
			if err != nil {
				select {
				case s.errCh <- fmt.Errorf("hpack decode: %w", err):
				default:
				}
				continue
			}

			hdr := http.Header{}
			statusCode := 0
			for _, hf := range decoded {
				if hf.Name == ":status" {
					statusCode, _ = strconv.Atoi(hf.Value)
				} else if !strings.HasPrefix(hf.Name, ":") {
					hdr.Add(hf.Name, hf.Value)
				}
			}
			if statusCode > 0 {
				hdr.Set("X-Status-Code", strconv.Itoa(statusCode))
			}

			if f.StreamEnded() {
				s.endStream = true
			}
			s.endHeaders = true

			// Send headers to the request handler
			select {
			case s.headersCh <- hdr:
			case <-c.readDone:
				return
			}

		case *http2.DataFrame:
			c.mu.Lock()
			s, ok := c.streams[f.StreamID]
			c.mu.Unlock()
			if !ok {
				continue
			}

			if f.StreamEnded() {
				s.endStream = true
			}

			// Send body chunk to the request handler
			select {
			case s.bodyCh <- f.Data():
			case <-c.readDone:
				return
			}

			// Send flow control credit back (WINDOW_UPDATE) if we consumed data
			if len(f.Data()) > 0 {
				c.framer.WriteWindowUpdate(f.StreamID, uint32(len(f.Data())))
				c.framer.WriteWindowUpdate(0, uint32(len(f.Data())))
			}
		}
	}
}

// doRequest sends a POST request and waits for the complete response.
func (c *bunH2Conn) doRequest(ctx context.Context, method, authority, path string, headers http.Header, body []byte) (*http.Response, error) {
	c.mu.Lock()
	streamID := c.nextID
	c.nextID += 2
	stream := &h2Stream{
		id:        streamID,
		headersCh: make(chan http.Header, 1),
		bodyCh:    make(chan []byte, 16),
		errCh:     make(chan error, 1),
	}
	c.streams[streamID] = stream
	c.mu.Unlock()

	// Clean up stream when done
	defer func() {
		c.mu.Lock()
		delete(c.streams, streamID)
		c.mu.Unlock()
	}()

	// Encode headers with Bun's pseudo-header order: :method, :scheme, :authority, :path
	var encBuf bytes.Buffer
	enc := hpack.NewEncoder(&encBuf)
	enc.WriteField(hpack.HeaderField{Name: ":method", Value: method})
	enc.WriteField(hpack.HeaderField{Name: ":scheme", Value: "https"})
	enc.WriteField(hpack.HeaderField{Name: ":authority", Value: authority})
	enc.WriteField(hpack.HeaderField{Name: ":path", Value: path})

	// Write regular headers (lowercase, skip hop-by-hop)
	for name, values := range headers {
		lower := strings.ToLower(name)
		// Skip hop-by-hop headers and pseudo-headers
		switch lower {
		case "connection", "keep-alive", "proxy-connection", "transfer-encoding", "upgrade", "host", "te", "content-length":
			continue
		}
		for _, value := range values {
			enc.WriteField(hpack.HeaderField{Name: lower, Value: value})
		}
	}

	// Write HEADERS frame
	endStream := len(body) == 0
	err := c.framer.WriteHeaders(http2.HeadersFrameParam{
		StreamID:      streamID,
		BlockFragment: encBuf.Bytes(),
		EndStream:     endStream,
		EndHeaders:    true,
	})
	if err != nil {
		return nil, fmt.Errorf("write headers: %w", err)
	}

	// Write body if present
	if len(body) > 0 {
		// Split into frames respecting max frame size (use 16384 = spec default)
		maxFrame := 16384
		remaining := body
		for len(remaining) > 0 {
			chunk := remaining
			if len(chunk) > maxFrame {
				chunk = chunk[:maxFrame]
			}
			remaining = remaining[len(chunk):]
			isLast := len(remaining) == 0
			if err := c.framer.WriteData(streamID, isLast, chunk); err != nil {
				return nil, fmt.Errorf("write data: %w", err)
			}
		}
	}

	// Wait for response headers
	select {
	case hdr := <-stream.headersCh:
		statusStr := hdr.Get("X-Status-Code")
		statusCode, _ := strconv.Atoi(statusStr)
		hdr.Del("X-Status-Code")

		// Collect body
		var bodyBuf bytes.Buffer
	loop:
		for {
			select {
			case chunk := <-stream.bodyCh:
				bodyBuf.Write(chunk)
				if stream.endStream {
					break loop
				}
			case err := <-stream.errCh:
				return nil, err
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		// Build http.Response
		resp := &http.Response{
			Status:     fmt.Sprintf("%d %s", statusCode, http.StatusText(statusCode)),
			StatusCode: statusCode,
			Header:     hdr,
			Body:       io.NopCloser(bytes.NewReader(bodyBuf.Bytes())),
			Proto:      "HTTP/2.0",
			ProtoMajor: 2,
			ProtoMinor: 0,
		}
		return resp, nil

	case err := <-stream.errCh:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.readDone:
		return nil, fmt.Errorf("connection closed during request")
	}
}

func (c *bunH2Conn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()
	return c.conn.Close()
}

// bunH2Transport implements http.RoundTripper with Bun-compatible HTTP/2 framing.
type bunH2Transport struct {
	baseURL string
	host    string
	port    int
	mu      sync.Mutex
	conn    *bunH2Conn
}

func newBunH2Transport(baseURL string) http.RoundTripper {
	parsed, _ := url.Parse(baseURL)
	host := parsed.Hostname()
	port := 443
	if parsed.Port() != "" {
		port, _ = strconv.Atoi(parsed.Port())
	}
	return &bunH2Transport{
		baseURL: baseURL,
		host:    host,
		port:    port,
	}
}

func (t *bunH2Transport) getConn(ctx context.Context) (*bunH2Conn, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.conn != nil && !t.conn.closed {
		return t.conn, nil
	}
	conn, err := dialBunH2(ctx, t.host, t.port)
	if err != nil {
		return nil, err
	}
	t.conn = conn
	return conn, nil
}

func (t *bunH2Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	conn, err := t.getConn(ctx)
	if err != nil {
		return nil, err
	}

	// Build path
	path := req.URL.Path
	if req.URL.RawQuery != "" {
		path += "?" + req.URL.RawQuery
	}

	// Read body
	var body []byte
	if req.Body != nil {
		body, err = io.ReadAll(req.Body)
		req.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read request body: %w", err)
		}
	}

	resp, err := conn.doRequest(ctx, req.Method, t.host, path, req.Header, body)
	if err != nil {
		// Connection might be dead, invalidate it
		t.mu.Lock()
		if t.conn == conn {
			t.conn = nil
		}
		t.mu.Unlock()
		conn.Close()
		return nil, err
	}
	return resp, nil
}

// Suppress unused warnings
var _ = binary.BigEndian
var _ = url.Parse
