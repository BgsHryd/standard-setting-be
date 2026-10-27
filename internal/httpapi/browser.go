package httpapi

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"standard-setting/backend/internal/auth"
	"standard-setting/backend/internal/session"
)

func (h *Handler) cookieName() string {
	if h.secureCookies {
		return "__Host-ss_session"
	}
	return "ss_session"
}

func (h *Handler) setBrowserCookie(c *fiber.Ctx, result auth.BrowserResult) {
	c.Cookie(&fiber.Cookie{Name: h.cookieName(), Value: result.CookieToken, Path: "/", HTTPOnly: true, Secure: h.secureCookies, SameSite: "Lax", MaxAge: max(1, int(math.Ceil(time.Until(time.UnixMilli(result.ExpiresAt)).Seconds()))), Expires: time.UnixMilli(result.ExpiresAt)})
}

func (h *Handler) browserOrigin(c *fiber.Ctx) error {
	origin := c.Get("Origin")
	if origin == "" || h.allowedOrigin == nil || !h.allowedOrigin(origin) {
		return errorResponse(c, 403, "asal permintaan tidak diizinkan")
	}
	if c.Path() == "/api/v1/auth/login" && !strings.EqualFold(strings.TrimSpace(strings.Split(c.Get("Content-Type"), ";")[0]), "application/json") {
		return errorResponse(c, 415, "login memerlukan application/json")
	}
	return c.Next()
}

func (h *Handler) browserCSRF(c *fiber.Ctx) error {
	switch c.Method() {
	case "GET", "HEAD", "OPTIONS":
		return c.Next()
	}
	origin := c.Get("Origin")
	if origin == "" || h.allowedOrigin == nil || !h.allowedOrigin(origin) {
		return errorResponse(c, 403, "asal permintaan tidak diizinkan")
	}
	rec, ok := c.Locals("browserSession").(session.BrowserSession)
	if !ok || rec.CSRFToken == "" || subtle.ConstantTimeCompare([]byte(c.Get("X-CSRF-Token")), []byte(rec.CSRFToken)) != 1 {
		return c.Status(403).JSON(fiber.Map{"status": "error", "code": "CSRF_INVALID", "message": "Verifikasi sesi gagal. Muat ulang halaman."})
	}
	return c.Next()
}

func (h *Handler) browserState(c *fiber.Ctx) error {
	result, err := h.auth.BrowserState(c.UserContext(), c.Cookies(h.cookieName()))
	if err != nil {
		return authError(c, err, 401)
	}
	return success(c, result)
}

func (h *Handler) browserLogout(c *fiber.Ctx) error {
	if err := h.auth.LogoutBrowser(c.UserContext(), c.Cookies(h.cookieName())); err != nil {
		return authError(c, err, 401)
	}
	// Revocation is authoritative in Redis. Do not emit Set-Cookie here: a
	// delayed logout response could otherwise erase a newer login's cookie.
	// The revoked cookie expires at its original deadline or is replaced at login.
	return success(c, fiber.Map{"loggedOut": true})
}

func (h *Handler) browserEvents(c *fiber.Ctx) error {
	// Fiber request buffers are reused after the handler returns; retain a copy.
	token := strings.Clone(c.Cookies(h.cookieName()))
	rec, _, err := h.auth.AuthenticateBrowser(c.UserContext(), token)
	if err != nil {
		return authError(c, err, 401)
	}
	ctx, cancel := context.WithCancel(context.Background())
	events, closeSub, err := h.auth.WatchBrowserSessions(ctx, token)
	if err != nil {
		cancel()
		return authError(c, err, 401)
	}
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-store")
	c.Set("X-Accel-Buffering", "no")
	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		defer cancel()
		defer closeSub()
		if _, err := fmt.Fprint(w, ": connected\n\n"); err != nil {
			return
		}
		if err := w.Flush(); err != nil {
			return
		}
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		deadline := time.NewTimer(max(time.Millisecond, time.Until(time.UnixMilli(rec.ExpiresAt))))
		defer deadline.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-events:
				if !ok {
					return
				}
			case <-ticker.C:
			case <-deadline.C:
			}
			checkCtx, stop := context.WithTimeout(ctx, 5*time.Second)
			_, _, err := h.auth.AuthenticateBrowser(checkCtx, token)
			stop()
			if err != nil {
				code, message := "SESSION_EXPIRED", "Sesi Anda telah berakhir. Silakan masuk kembali."
				if errors.Is(err, auth.ErrSessionReplaced) {
					code, message = "SESSION_REPLACED", "Anda telah login di perangkat lain"
				}
				if errors.Is(err, auth.ErrSecurityUnavailable) {
					code, message = "SECURITY_UNAVAILABLE", "layanan keamanan sementara tidak tersedia"
				}
				data, _ := json.Marshal(fiber.Map{"code": code, "message": message, "sessionId": rec.ID})
				_, _ = fmt.Fprintf(w, "event: session\ndata: %s\n\n", data)
				_ = w.Flush()
				return
			}
			if _, err = fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			if err = w.Flush(); err != nil {
				return
			}
		}
	})
	return nil
}
