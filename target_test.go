package main

import (
	"net/http"
	"reflect"
	"testing"
)

func TestInferBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		host    string
		headers map[string]string
		want    string
	}{
		{
			name: "trusted forwarded headers",
			host: "local.proxy:8080",
			headers: map[string]string{
				"X-Forwarded-Proto": "https",
				"X-Forwarded-Host":  "proxy.example.com",
			},
			want: "https://proxy.example.com",
		},
		{
			name: "fallback on invalid proto",
			host: "local.proxy:8080",
			headers: map[string]string{
				"X-Forwarded-Proto": "javascript",
				"X-Forwarded-Host":  "proxy.example.com",
			},
			want: "http://proxy.example.com",
		},
		{
			name: "fallback on invalid host",
			host: "local.proxy:8080",
			headers: map[string]string{
				"X-Forwarded-Proto": "https",
				"X-Forwarded-Host":  "bad/host",
			},
			want: "https://local.proxy:8080",
		},
		{
			name: "take first forwarded value",
			host: "local.proxy:8080",
			headers: map[string]string{
				"X-Forwarded-Proto": "HTTPS, http",
				"X-Forwarded-Host":  "Proxy.Example.com, ignored.example.com",
			},
			want: "https://proxy.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &http.Request{Header: make(http.Header), Host: tt.host}
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			if got := inferBaseURL(req); got != tt.want {
				t.Fatalf("inferBaseURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUnproxyURLPreservesQuery(t *testing.T) {
	raw := "https://proxy.example.com/https/emby.example.com/443/web/index.html?api_key=abc123&userId=42"
	want := "https://emby.example.com/web/index.html?api_key=abc123&userId=42"

	if got := unproxyURL(raw); got != want {
		t.Fatalf("unproxyURL() = %q, want %q", got, want)
	}
}

func TestParseTarget(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		query   string
		want    *target
		wantErr bool
	}{
		{
			name:  "https with path and query",
			path:  "/https/emby.example.com/443/web/index.html",
			query: "api_key=abc",
			want: &target{
				Scheme: "https",
				Domain: "emby.example.com",
				Port:   443,
				Path:   "web/index.html",
				Query:  "api_key=abc",
			},
		},
		{
			name:  "http without trailing path",
			path:  "/http/emby.example.com/8096",
			query: "",
			want: &target{
				Scheme: "http",
				Domain: "emby.example.com",
				Port:   8096,
				Path:   "",
				Query:  "",
			},
		},
		{name: "invalid scheme", path: "/ftp/emby.example.com/21/file", wantErr: true},
		{name: "invalid port", path: "/https/emby.example.com/not-a-port/file", wantErr: true},
		{name: "missing domain", path: "/https//443/file", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTarget(tt.path, tt.query)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseTarget(%q, %q) expected error", tt.path, tt.query)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseTarget(%q, %q) unexpected error: %v", tt.path, tt.query, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseTarget(%q, %q) = %#v, want %#v", tt.path, tt.query, got, tt.want)
			}
		})
	}
}

func TestBuildTargetURL(t *testing.T) {
	tests := []struct {
		name string
		in   *target
		want string
	}{
		{
			name: "with query",
			in: &target{
				Scheme: "https",
				Domain: "emby.example.com",
				Port:   443,
				Path:   "web/index.html",
				Query:  "api_key=abc",
			},
			want: "https://emby.example.com:443/web/index.html?api_key=abc",
		},
		{
			name: "root path",
			in: &target{
				Scheme: "http",
				Domain: "emby.example.com",
				Port:   8096,
				Path:   "",
				Query:  "",
			},
			want: "http://emby.example.com:8096/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildTargetURL(tt.in); got != tt.want {
				t.Fatalf("buildTargetURL() = %q, want %q", got, tt.want)
			}
		})
	}
}
