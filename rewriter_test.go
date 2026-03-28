package main

import "testing"

func TestShouldRewriteBody(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		want        bool
	}{
		{name: "json with charset", contentType: "application/json; charset=utf-8", want: true},
		{name: "html", contentType: "text/html", want: true},
		{name: "xml", contentType: "application/xml", want: true},
		{name: "image png", contentType: "image/png", want: false},
		{name: "gzip archive", contentType: "application/gzip", want: false},
		{name: "invalid keeps false", contentType: "@@@", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldRewriteBody(tt.contentType); got != tt.want {
				t.Fatalf("shouldRewriteBody(%q) = %v, want %v", tt.contentType, got, tt.want)
			}
		})
	}
}

func TestRewriteSingleURL(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		baseURL string
		want    string
	}{
		{
			name:    "rewrite https default port",
			rawURL:  "https://emby.example.com/web/index.html?x=1",
			baseURL: "https://proxy.example.com",
			want:    "https://proxy.example.com/https/emby.example.com/443/web/index.html?x=1",
		},
		{
			name:    "rewrite http custom port",
			rawURL:  "http://emby.example.com:8096/Items/1",
			baseURL: "https://proxy.example.com",
			want:    "https://proxy.example.com/http/emby.example.com/8096/Items/1",
		},
		{
			name:    "leave relative URL untouched",
			rawURL:  "/web/index.html",
			baseURL: "https://proxy.example.com",
			want:    "/web/index.html",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rewriteSingleURL(tt.rawURL, tt.baseURL); got != tt.want {
				t.Fatalf("rewriteSingleURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRewriteBody(t *testing.T) {
	body := []byte(`{"api":"https://emby.example.com/Items/1","fallback":"http://cdn.example.com:8096/video.mp4","relative":"/Items/2"}`)
	baseURL := "https://proxy.example.com"
	want := `{"api":"https://proxy.example.com/https/emby.example.com/443/Items/1","fallback":"https://proxy.example.com/http/cdn.example.com/8096/video.mp4","relative":"/Items/2"}`

	if got := string(rewriteBody(body, baseURL)); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}
