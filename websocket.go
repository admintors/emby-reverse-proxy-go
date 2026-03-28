package main

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const webSocketHandshakeTimeout = 10 * time.Second

func (h *ProxyHandler) serveWebSocket(w http.ResponseWriter, r *http.Request, t *target, start time.Time) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "websocket not supported", http.StatusInternalServerError)
		return
	}

	clientConn, clientRW, err := hj.Hijack()
	if err != nil {
		log.Printf("[ERROR] hijack websocket connection failed: %v", err)
		http.Error(w, "websocket hijack failed", http.StatusInternalServerError)
		return
	}
	defer clientConn.Close()
	_ = clientConn.SetDeadline(time.Now().Add(webSocketHandshakeTimeout))

	upstreamConn, err := dialTargetConn(t)
	if err != nil {
		log.Printf("[ERROR] websocket dial %s/%s failed: %v", t.Domain, t.Path, err)
		writeHijackedHTTPError(clientRW, http.StatusBadGateway, "upstream websocket connection failed")
		return
	}
	defer upstreamConn.Close()
	_ = upstreamConn.SetDeadline(time.Now().Add(webSocketHandshakeTimeout))

	if err := writeWebSocketRequest(upstreamConn, r, t); err != nil {
		log.Printf("[ERROR] websocket request write %s/%s failed: %v", t.Domain, t.Path, err)
		writeHijackedHTTPError(clientRW, http.StatusBadGateway, "upstream websocket handshake failed")
		return
	}

	upstreamReader := bufio.NewReader(upstreamConn)
	resp, err := http.ReadResponse(upstreamReader, r)
	if err != nil {
		log.Printf("[ERROR] websocket response read %s/%s failed: %v", t.Domain, t.Path, err)
		writeHijackedHTTPError(clientRW, http.StatusBadGateway, "invalid upstream websocket response")
		return
	}

	statusLine := fmt.Sprintf("HTTP/1.1 %d %s\r\n", resp.StatusCode, http.StatusText(resp.StatusCode))
	if _, err := clientRW.WriteString(statusLine); err != nil {
		log.Printf("[ERROR] websocket response status write %s/%s failed: %v", t.Domain, t.Path, err)
		resp.Body.Close()
		return
	}
	if err := resp.Header.Write(clientRW); err != nil {
		log.Printf("[ERROR] websocket response header write %s/%s failed: %v", t.Domain, t.Path, err)
		resp.Body.Close()
		return
	}
	if _, err := clientRW.WriteString("\r\n"); err != nil {
		log.Printf("[ERROR] websocket response header terminator write %s/%s failed: %v", t.Domain, t.Path, err)
		resp.Body.Close()
		return
	}
	if err := clientRW.Flush(); err != nil {
		log.Printf("[ERROR] websocket client flush %s/%s failed: %v", t.Domain, t.Path, err)
		resp.Body.Close()
		return
	}

	if resp.StatusCode != http.StatusSwitchingProtocols {
		if err := copyResponseBodyToHijackedClient(clientRW, resp.Body); err != nil {
			log.Printf("[ERROR] websocket rejected response body write %s/%s failed: %v", t.Domain, t.Path, err)
		}
		resp.Body.Close()
		log.Printf("[PROXY] %d %s %s/%s | websocket upgrade rejected | %s",
			resp.StatusCode, r.Method, t.Domain, t.Path, time.Since(start))
		return
	}
	resp.Body.Close()

	clientBuffered, err := drainBufferedReader(clientRW.Reader, upstreamConn)
	if err != nil {
		log.Printf("[ERROR] websocket buffered client drain %s/%s failed: %v", t.Domain, t.Path, err)
		return
	}
	upstreamBuffered, err := drainBufferedReader(upstreamReader, clientConn)
	if err != nil {
		log.Printf("[ERROR] websocket buffered upstream drain %s/%s failed: %v", t.Domain, t.Path, err)
		return
	}

	_ = clientConn.SetDeadline(time.Time{})
	_ = upstreamConn.SetDeadline(time.Time{})
	bytesUp, bytesDown := proxyWebSocketStreams(clientConn, upstreamConn)
	bytesUp += clientBuffered
	bytesDown += upstreamBuffered
	log.Printf("[WS] %d %s %s/%s | up %s | down %s | %s",
		resp.StatusCode, r.Method, t.Domain, t.Path,
		formatBytes(bytesUp), formatBytes(bytesDown), time.Since(start))
}

func isWebSocketRequest(r *http.Request) bool {
	return headerContainsToken(r.Header, "Connection", "upgrade") && strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket")
}

func dialTargetConn(t *target) (net.Conn, error) {
	addr := net.JoinHostPort(t.Domain, strconv.Itoa(t.Port))
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 60 * time.Second}
	if t.Scheme == "https" {
		return tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: t.Domain})
	}
	return dialer.Dial("tcp", addr)
}

func writeWebSocketRequest(conn net.Conn, r *http.Request, t *target) error {
	req := &http.Request{
		Method:     r.Method,
		URL:        &url.URL{Path: targetRequestPath(t), RawQuery: t.Query},
		Host:       targetHostPort(t),
		Header:     make(http.Header, len(r.Header)),
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
	}
	copyRequestHeaders(req.Header, r.Header, true)
	setUpstreamHost(req, t)
	rewriteProxySensitiveRequestHeaders(req.Header)
	return req.Write(conn)
}

func proxyWebSocketStreams(clientConn, upstreamConn net.Conn) (int64, int64) {
	var wg sync.WaitGroup
	var upstreamBytes int64
	var downstreamBytes int64

	wg.Add(2)
	go func() {
		defer wg.Done()
		bufp := copyBufPool.Get().(*[]byte)
		defer copyBufPool.Put(bufp)
		written, err := io.CopyBuffer(upstreamConn, clientConn, *bufp)
		upstreamBytes = written
		if err != nil && !isNetClosedError(err) {
			log.Printf("[ERROR] websocket upstream copy failed: %v", err)
		}
		_ = upstreamConn.SetReadDeadline(time.Now())
	}()
	go func() {
		defer wg.Done()
		bufp := copyBufPool.Get().(*[]byte)
		defer copyBufPool.Put(bufp)
		written, err := io.CopyBuffer(clientConn, upstreamConn, *bufp)
		downstreamBytes = written
		if err != nil && !isNetClosedError(err) {
			log.Printf("[ERROR] websocket downstream copy failed: %v", err)
		}
		_ = clientConn.SetReadDeadline(time.Now())
	}()
	wg.Wait()
	return upstreamBytes, downstreamBytes
}

func drainBufferedReader(r *bufio.Reader, dst net.Conn) (int64, error) {
	buffered := r.Buffered()
	if buffered == 0 {
		return 0, nil
	}
	buf, err := r.Peek(buffered)
	if err != nil {
		return 0, err
	}
	written, err := dst.Write(buf)
	if err != nil {
		return int64(written), err
	}
	_, _ = r.Discard(written)
	return int64(written), nil
}

func copyResponseBodyToHijackedClient(rw *bufio.ReadWriter, body io.Reader) error {
	if body == nil {
		return nil
	}
	bufp := copyBufPool.Get().(*[]byte)
	defer copyBufPool.Put(bufp)
	_, err := io.CopyBuffer(rw, body, *bufp)
	if err != nil {
		return err
	}
	return rw.Flush()
}

func writeHijackedHTTPError(rw *bufio.ReadWriter, statusCode int, message string) {
	statusText := http.StatusText(statusCode)
	if statusText == "" {
		statusText = "Error"
	}
	_, _ = rw.WriteString(fmt.Sprintf("HTTP/1.1 %d %s\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", statusCode, statusText, len(message), message))
	_ = rw.Flush()
}

func isNetClosedError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "use of closed network connection") || strings.Contains(msg, "closed pipe") || strings.Contains(msg, "broken pipe") || strings.Contains(msg, "connection reset by peer") || strings.Contains(msg, "eof")
}
