package httpapi

import (
	"encoding/json"
	"golang.org/x/crypto/bcrypt"
	"net/http/httptest"
	"standard-setting/backend/internal/auth"
	"standard-setting/backend/internal/domain"
	"standard-setting/backend/internal/service"
	"strings"
	"testing"
	"time"
)

func TestQuestionExampleFlagRejectsNestedNullBeforePersistence(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("cookie-password"), bcrypt.MinCost)
	users := &testUsers{user: domain.User{ID: "cookie-user", Username: "cookie-user", PasswordHash: string(hash), Active: true, Role: domain.RoleAdminSuper}}
	svc := auth.NewService(users, &cookieTestStore{}, "test-secret", time.Minute, time.Hour)
	// No store is needed: invalid flags must be rejected before persistence.
	app := New(Config{Auth: svc, Service: service.New(nil, nil, ""), AllowedOrigins: "https://app.example.test", SecureCookies: true})
	cookie, state := cookieLogin(t, app)
	raw, _ := json.Marshal(map[string]any{"version": 0, "instructions": domain.DefaultQuestionInstructions(), "tampilkan_soal_contoh": map[string]any{"pilihan_ganda_kompleks_mcma": nil}})
	req := httptest.NewRequest("PUT", "/api/v1/config/question-instructions", strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://app.example.test")
	req.Header.Set("X-CSRF-Token", state.CSRFToken)
	req.Header.Set("X-Session-ID", state.SessionID)
	req.AddCookie(cookie)
	res, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 422 {
		t.Fatalf("null flag reached persistence: status=%d", res.StatusCode)
	}
}
