package main

import (
	"net"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/valyala/fasthttp"
)

func TestPanelFiberTrustedProxyClientIP(t *testing.T) {
	app := fiber.New(panelFiberConfig())
	app.Get("/ip", func(c *fiber.Ctx) error {
		return c.SendString(c.IP())
	})

	run := func(remoteIP, xff string) string {
		var fctx fasthttp.RequestCtx
		fctx.Request.Header.SetMethod("GET")
		fctx.Request.SetRequestURI("/ip")
		if xff != "" {
			fctx.Request.Header.Set("X-Forwarded-For", xff)
		}
		ip := net.ParseIP(remoteIP)
		if ip == nil {
			t.Fatalf("bad test remote IP %q", remoteIP)
		}
		fctx.SetRemoteAddr(&net.TCPAddr{IP: ip, Port: 43210})
		app.Handler()(&fctx)
		return string(fctx.Response.Body())
	}

	// 1. Request from local Caddy bridge IP (RFC1918) uses forwarded client IP.
	if got := run("172.18.0.2", "198.51.100.42"); got != "198.51.100.42" {
		t.Fatalf("trusted bridge proxy: got %q want 198.51.100.42", got)
	}

	// 2. Direct external client bypassing Caddy cannot spoof X-Forwarded-For.
	if got := run("203.0.113.99", "198.51.100.42"); got != "203.0.113.99" {
		t.Fatalf("untrusted public peer spoofed XFF: got %q want 203.0.113.99", got)
	}

	// 3. Trusted loopback with invalid XFF falls back to peer IP.
	if got := run("127.0.0.1", "not-an-ip"); got != "127.0.0.1" {
		t.Fatalf("invalid XFF fallback: got %q want 127.0.0.1", got)
	}

	// 4. Trusted IPv6 loopback also resolves IPv6 forwarded client IP.
	if got := run("::1", "2001:db8::7"); got != "2001:db8::7" {
		t.Fatalf("trusted IPv6 loopback: got %q want 2001:db8::7", got)
	}
}
