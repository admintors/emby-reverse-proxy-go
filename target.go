package main

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type target struct {
	Scheme string
	Domain string
	Port   int
	Path   string
	Query  string
}

func parseTarget(path, query string) (*target, error) {
	trimmed := strings.TrimPrefix(path, "/")
	if trimmed == "" {
		return nil, fmt.Errorf("usage: /{scheme}/{domain}/{port}/{path}")
	}
	parts := strings.SplitN(trimmed, "/", 4)
	if len(parts) < 3 {
		return nil, fmt.Errorf("usage: /{scheme}/{domain}/{port}/{path}")
	}
	scheme := strings.ToLower(parts[0])
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("scheme must be http or https, got: %s", scheme)
	}
	domain := parts[1]
	if domain == "" {
		return nil, fmt.Errorf("domain is required")
	}
	port, err := strconv.Atoi(parts[2])
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid port: %s", parts[2])
	}
	remaining := ""
	if len(parts) == 4 {
		remaining = parts[3]
	}
	return &target{Scheme: scheme, Domain: domain, Port: port, Path: remaining, Query: query}, nil
}

func buildTargetURL(t *target) string {
	var b strings.Builder
	b.Grow(len(t.Scheme) + 3 + len(t.Domain) + 6 + 1 + len(t.Path) + 1 + len(t.Query))
	b.WriteString(t.Scheme)
	b.WriteString("://")
	b.WriteString(t.Domain)
	b.WriteByte(':')
	b.WriteString(strconv.Itoa(t.Port))
	b.WriteString(targetRequestPath(t))
	if t.Query != "" {
		b.WriteByte('?')
		b.WriteString(t.Query)
	}
	return b.String()
}

func targetHostPort(t *target) string {
	if isDefaultPort(t.Scheme, t.Port) {
		return t.Domain
	}
	return net.JoinHostPort(t.Domain, strconv.Itoa(t.Port))
}

func targetRequestPath(t *target) string {
	if t.Path == "" {
		return "/"
	}
	return "/" + t.Path
}

func inferBaseURL(r *http.Request) string {
	scheme := "http"
	if proto := firstHeaderValue(r.Header.Get("X-Forwarded-Proto")); proto == "http" || proto == "https" {
		scheme = proto
	}
	host := firstHeaderValue(r.Header.Get("X-Forwarded-Host"))
	if host == "" || strings.ContainsAny(host, "/\\ 	\r\n") {
		host = r.Host
	}
	return scheme + "://" + host
}

func isDefaultPort(scheme string, port int) bool {
	return (scheme == "https" && port == 443) || (scheme == "http" && port == 80)
}

func firstHeaderValue(raw string) string {
	if raw == "" {
		return ""
	}
	if idx := strings.IndexByte(raw, ','); idx >= 0 {
		raw = raw[:idx]
	}
	return strings.TrimSpace(strings.ToLower(raw))
}

func unproxyURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	t, err := parseTarget(parsed.Path, parsed.RawQuery)
	if err != nil {
		return raw
	}
	var b strings.Builder
	b.WriteString(t.Scheme)
	b.WriteString("://")
	b.WriteString(t.Domain)
	if !isDefaultPort(t.Scheme, t.Port) {
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(t.Port))
	}
	b.WriteString(targetRequestPath(t))
	if t.Query != "" {
		b.WriteByte('?')
		b.WriteString(t.Query)
	}
	return b.String()
}
