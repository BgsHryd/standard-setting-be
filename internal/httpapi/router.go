package httpapi

import (
	"errors"
	"github.com/google/uuid"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"standard-setting/backend/internal/auth"
	"standard-setting/backend/internal/domain"
	"standard-setting/backend/internal/service"
)

type App = fiber.App

type Config struct {
	Auth           *auth.Service
	Service        *service.Service
	AllowedOrigins string
	TrustedProxies []string
	SecureCookies  bool
}

type Handler struct {
	auth          *auth.Service
	service       *service.Service
	secureCookies bool
	allowedOrigin func(string) bool
}

func New(config Config) *App {
	trusted := []string{}
	for _, entry := range config.TrustedProxies {
		entry = strings.TrimSpace(entry)
		if net.ParseIP(entry) != nil {
			trusted = append(trusted, entry)
		} else if _, _, err := net.ParseCIDR(entry); err == nil {
			trusted = append(trusted, entry)
		}
	}
	app := fiber.New(fiber.Config{EnableTrustedProxyCheck: len(trusted) > 0, TrustedProxies: trusted, ProxyHeader: func() string {
		if len(trusted) > 0 {
			return fiber.HeaderXForwardedFor
		}
		return ""
	}(), BodyLimit: 6 * 1024 * 1024, ErrorHandler: func(c *fiber.Ctx, err error) error {
		var fiberError *fiber.Error
		if errors.As(err, &fiberError) {
			return errorResponse(c, fiberError.Code, fiberError.Message)
		}
		return errorResponse(c, http.StatusInternalServerError, err.Error())
	}})
	app.Use(recover.New())
	app.Use(func(c *fiber.Ctx) error {
		id := uuid.NewString()
		c.Set("X-Request-ID", id)
		c.SetUserContext(domain.WithAuditRequestID(c.UserContext(), id))
		return c.Next()
	})
	corsConfig := buildCORSConfig(config.AllowedOrigins)
	app.Use(cors.New(corsConfig))
	handler := &Handler{auth: config.Auth, service: config.Service, secureCookies: config.SecureCookies, allowedOrigin: corsConfig.AllowOriginsFunc}
	api := app.Group("/api/v1")
	api.Use(func(c *fiber.Ctx) error { c.Set("Cache-Control", "no-store"); return c.Next() })
	api.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok", "service": "standard-setting"})
	})
	api.Post("/auth/login", handler.browserOrigin, handler.login)
	api.Get("/auth/session", handler.browserState)
	api.Get("/auth/events", handler.browserEvents)
	api.Post("/auth/activity", handler.authenticate, handler.browserCSRF, handler.browserActivity)
	api.Post("/auth/logout", handler.authenticate, handler.browserCSRF, handler.browserLogout)
	api.Post("/auth/change-password", handler.authenticate, handler.browserCSRF, handler.changePassword)

	protected := api.Group("", handler.authenticate, handler.browserCSRF, handler.passwordChanged)
	protected.Get("/me", handler.me)

	admin := protected.Group("")
	adminRole := handler.requireRoles(domain.RoleAdminSuper)
	admin.Get("/config/question-instructions", adminRole, handler.getQuestionInstructions)
	admin.Put("/config/question-instructions", adminRole, handler.saveQuestionInstructions)
	admin.Get("/projects", adminRole, handler.listProjects)
	admin.Post("/projects", adminRole, handler.saveProject)
	admin.Get("/projects/:id", adminRole, handler.getProject)
	admin.Put("/projects/:id", adminRole, handler.saveProject)
	admin.Delete("/projects/:id", adminRole, handler.deleteProject)
	admin.Post("/projects/:id/activate", adminRole, handler.activateProject)
	admin.Post("/projects/:id/complete", adminRole, handler.completeProject)
	admin.Post("/projects/:id/accounts/generate", adminRole, handler.generateAccounts)
	admin.Get("/subjects", adminRole, handler.listSubjects)
	admin.Post("/subjects", adminRole, handler.createSubject)
	admin.Get("/projects/:id/accounts", adminRole, handler.accountRows)
	admin.Get("/projects/:id/templates/:step", adminRole, handler.template)
	admin.Post("/projects/:id/imports/:step", adminRole, handler.importWorkbook)
	admin.Get("/projects/:id/monitoring/all", adminRole, handler.monitoringAll)
	admin.Patch("/users/:id/active", adminRole, handler.userActive)
	admin.Post("/users/:id/reset-password", adminRole, handler.resetPassword)
	admin.Get("/users", adminRole, handler.listUsers)
	admin.Get("/items", adminRole, handler.listItems)
	admin.Post("/items", adminRole, handler.saveItems)
	admin.Get("/items/:id/result", adminRole, handler.itemResult)
	admin.Get("/items/:id", adminRole, handler.itemDetail)
	admin.Get("/projects/:id/monitoring", adminRole, handler.adminMonitoring)
	admin.Get("/projects/:id/panelists/:panelistId/submissions", adminRole, handler.adminPreviewPanelist)

	pic := protected.Group("/pic", handler.requireRoles(domain.RolePICMapel))
	pic.Get("/monitoring", handler.picMonitoring)
	pic.Get("/round-results", handler.picRoundOverview)
	pic.Put("/panelist-display", handler.setPanelistDisplay)
	pic.Get("/panelists/:panelistId/submissions", handler.previewPanelist)
	pic.Post("/rounds/:action", handler.roundAction)

	panelist := protected.Group("/panelist", handler.requireRoles(domain.RolePanelist))
	panelist.Get("/form", handler.panelistForm)
	panelist.Put("/submissions/:itemId", handler.saveSubmission)
	handler.registerCoreRoutes(protected, admin, adminRole, pic, panelist)
	handler.registerWorkflowRoutes(pic, panelist)
	handler.registerPICItemResultRoutes(pic)
	handler.registerAccountManagementRoutes(admin, adminRole)
	handler.registerProjectWorkbookRoutes(admin, adminRole)
	handler.registerCatalogWorkbookRoutes(admin, adminRole)
	handler.registerTelemetryRoutes(pic, panelist)
	handler.registerReportingRoutes(admin, adminRole, panelist)
	handler.registerSecurityRoutes(admin, adminRole)

	return app
}

func buildCORSConfig(allowedOrigins string) cors.Config {
	exactOrigins := make(map[string]struct{})
	localPorts := make(map[string]struct{})
	for _, configured := range strings.Split(allowedOrigins, ",") {
		origin := strings.ToLower(strings.TrimSpace(configured))
		if origin == "" {
			continue
		}
		exactOrigins[origin] = struct{}{}
		parsed, err := url.Parse(origin)
		if err == nil && isLocalHost(parsed.Hostname()) && parsed.Port() != "" {
			localPorts[parsed.Port()] = struct{}{}
		}
	}

	return cors.Config{
		AllowOriginsFunc: func(origin string) bool {
			origin = strings.ToLower(strings.TrimSpace(origin))
			if _, allowed := exactOrigins[origin]; allowed {
				return true
			}
			parsed, err := url.Parse(origin)
			if err != nil || parsed.Scheme != "http" || !isLocalHost(parsed.Hostname()) {
				return false
			}
			_, allowedPort := localPorts[parsed.Port()]
			return allowedPort
		},
		AllowMethods:     "GET,POST,HEAD,PUT,DELETE,PATCH,OPTIONS",
		AllowHeaders:     "Origin,Content-Type,Accept,X-CSRF-Token,X-Session-ID",
		AllowCredentials: true,
		MaxAge:           600,
	}
}

func isLocalHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
}

func success(c *fiber.Ctx, data any) error {
	return c.JSON(fiber.Map{"status": "success", "data": data})
}

func errorResponse(c *fiber.Ctx, status int, message string) error {
	return c.Status(status).JSON(fiber.Map{"status": "error", "message": message})
}

func (h *Handler) requireService(c *fiber.Ctx) error {
	if h.service == nil {
		return fiber.NewError(http.StatusServiceUnavailable, "layanan data belum tersedia")
	}
	return nil
}

func (h *Handler) login(c *fiber.Ctx) error {
	var request struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.BodyParser(&request); err != nil || strings.TrimSpace(request.Username) == "" || request.Password == "" {
		return errorResponse(c, http.StatusUnprocessableEntity, "username dan password wajib diisi")
	}
	result, err := h.auth.LoginBrowser(c.UserContext(), strings.TrimSpace(request.Username), request.Password, c.IP())
	if err != nil {
		return authError(c, err, http.StatusUnauthorized)
	}
	h.setBrowserCookie(c, result)
	return success(c, result)
}

func (h *Handler) authenticate(c *fiber.Ctx) error {
	rec, user, err := h.auth.AuthenticateBrowser(c.UserContext(), c.Cookies(h.cookieName()))
	if err != nil {
		return authError(c, err, http.StatusUnauthorized)
	}
	if expected := c.Get("X-Session-ID"); expected != "" && expected != rec.ID {
		return c.Status(409).JSON(fiber.Map{"status": "error", "code": "SESSION_CHANGED", "message": "Sesi akun berubah. Muat ulang halaman."})
	}
	claims := &auth.Claims{AuthVersion: user.AuthVersion, Role: user.Role, ProjectID: user.ProjectID, SubjectCode: user.SubjectCode, Name: user.Name, Username: user.Username, MustChangePassword: user.MustChangePassword}
	claims.Subject = user.ID
	c.Locals("browserSession", rec)
	c.Locals("claims", claims)
	return c.Next()
}

func (h *Handler) passwordChanged(c *fiber.Ctx) error {
	claims := getClaims(c)
	if claims.MustChangePassword {
		return errorResponse(c, http.StatusPreconditionRequired, "password wajib diganti sebelum melanjutkan")
	}
	return c.Next()
}

func (h *Handler) requireRoles(roles ...domain.Role) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := getClaims(c)
		for _, role := range roles {
			if claims.Role == role {
				return c.Next()
			}
		}
		return errorResponse(c, http.StatusForbidden, "role tidak memiliki akses")
	}
}

func getClaims(c *fiber.Ctx) *auth.Claims {
	claims, _ := c.Locals("claims").(*auth.Claims)
	if claims == nil {
		return &auth.Claims{}
	}
	return claims
}

func (h *Handler) me(c *fiber.Ctx) error {
	claims := getClaims(c)
	u, err := h.auth.CurrentUser(c.UserContext(), claims.Subject)
	if err != nil {
		return authError(c, err, 401)
	}
	required := false
	if h.service != nil {
		required, err = h.service.ContactRequired(c.UserContext(), u.ID)
		if err != nil {
			return errorResponse(c, 503, "kebijakan kontak belum dapat diperiksa")
		}
	}
	return success(c, auth.UserView{ID: u.ID, Username: u.Username, Name: u.Name, Role: u.Role, ProjectID: u.ProjectID, ProjectName: u.ProjectName, SubjectCode: u.SubjectCode, MustChangePassword: u.MustChangePassword, ContactRequired: required})
}
func authError(c *fiber.Ctx, err error, fallback int) error {
	if errors.Is(err, auth.ErrOutsideAssessmentPeriod) {
		return c.Status(http.StatusForbidden).JSON(fiber.Map{"status": "error", "code": "OUTSIDE_ASSESSMENT_PERIOD", "message": err.Error()})
	}
	if errors.Is(err, auth.ErrSessionReplaced) {
		return c.Status(401).JSON(fiber.Map{"status": "error", "code": "SESSION_REPLACED", "message": "Anda telah login di perangkat lain"})
	}
	if errors.Is(err, auth.ErrInvalidSession) {
		return c.Status(401).JSON(fiber.Map{"status": "error", "code": "SESSION_EXPIRED", "message": "Sesi Anda telah berakhir. Silakan masuk kembali."})
	}
	var limited *auth.RateLimitError
	if errors.As(err, &limited) {
		c.Set("Retry-After", strconv.Itoa(max(1, int(math.Ceil(limited.RetryAfter.Seconds())))))
		return errorResponse(c, 429, "terlalu banyak percobaan login; coba lagi setelah jeda")
	}
	if errors.Is(err, auth.ErrSecurityUnavailable) {
		return errorResponse(c, 503, "layanan keamanan sementara tidak tersedia; coba kembali")
	}
	return errorResponse(c, fallback, err.Error())
}

func (h *Handler) changePassword(c *fiber.Ctx) error {
	var request struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := c.BodyParser(&request); err != nil {
		return errorResponse(c, http.StatusUnprocessableEntity, "payload tidak valid")
	}
	session, err := h.auth.ChangeBrowserPassword(c.UserContext(), c.Cookies(h.cookieName()), request.CurrentPassword, request.NewPassword)
	if err != nil {
		status := http.StatusUnprocessableEntity
		if errors.Is(err, auth.ErrInvalidSession) {
			status = http.StatusUnauthorized
		}
		return authError(c, err, status)
	}

	h.setBrowserCookie(c, session)
	return success(c, session)
}

func (h *Handler) listProjects(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	projects, err := h.service.ListProjects(c.UserContext())
	if err != nil {
		return err
	}
	return success(c, projects)
}

func (h *Handler) getProject(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	project, err := h.service.GetProject(c.UserContext(), c.Params("id"))
	if err != nil {
		return err
	}
	return success(c, project)
}

func (h *Handler) saveProject(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	var project domain.Project
	if err := c.BodyParser(&project); err != nil {
		return errorResponse(c, 422, "konfigurasi proyek tidak valid")
	}
	if id := c.Params("id"); id != "" {
		project.ID = id
	}
	project, err := h.service.SaveProject(c.UserContext(), project, getClaims(c).Subject)
	if err != nil {
		return errorResponse(c, 409, err.Error())
	}
	return success(c, project)
}

func (h *Handler) deleteProject(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	var request struct {
		Confirmation string `json:"confirmation"`
	}
	_ = c.BodyParser(&request)
	if err := h.service.DeleteProject(c.UserContext(), c.Params("id"), request.Confirmation); err != nil {
		return errorResponse(c, 409, err.Error())
	}
	return success(c, fiber.Map{"deleted": true})
}

func (h *Handler) activateProject(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	project, validation, err := h.service.ActivateProject(c.UserContext(), c.Params("id"))
	if err != nil {
		return err
	}
	if len(validation) > 0 {
		return c.Status(422).JSON(fiber.Map{"status": "error", "message": "konfigurasi proyek belum lengkap", "errors": validation})
	}
	return success(c, project)
}

func (h *Handler) completeProject(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	var request struct {
		Confirmation string `json:"confirmation"`
	}
	_ = c.BodyParser(&request)
	project, err := h.service.CompleteProject(c.UserContext(), c.Params("id"), request.Confirmation)
	if err != nil {
		return errorResponse(c, 409, err.Error())
	}
	return success(c, project)
}

func (h *Handler) generateAccounts(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	var request struct {
		Accounts []service.AccountInput `json:"accounts"`
	}
	if err := c.BodyParser(&request); err != nil {
		return errorResponse(c, 422, "daftar akun tidak valid")
	}
	users, err := h.service.GenerateAccounts(c.UserContext(), c.Params("id"), request.Accounts)
	if err != nil {
		return errorResponse(c, 409, err.Error())
	}
	return success(c, users)
}

func (h *Handler) listUsers(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	users, err := h.service.ListUsers(c.UserContext(), c.Query("projectId"))
	if err != nil {
		return err
	}
	return success(c, users)
}

func (h *Handler) listItems(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	items, err := h.service.ListItems(c.UserContext(), c.Query("subjectCode"), c.Query("projectId"), c.QueryBool("onlyAvailable", false))
	if err != nil {
		return err
	}
	return success(c, adminItems(items))
}

func (h *Handler) saveItems(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	var request struct {
		Items []catalogItemDTO `json:"items"`
	}
	if err := c.BodyParser(&request); err != nil {
		return errorResponse(c, 422, "daftar ID butir tidak valid")
	}
	items := make([]domain.Item, len(request.Items))
	for i, row := range request.Items {
		items[i] = row.Item
		items[i].KJ, items[i].Measure = row.KJ, row.Measure
		items[i].MeasureCategory = row.MeasureCategory
	}
	if err := h.service.UpsertItems(c.UserContext(), items); err != nil {
		return errorResponse(c, 409, err.Error())
	}
	return success(c, adminItems(items))
}

func (h *Handler) itemResult(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	result, err := h.service.ItemResult(c.UserContext(), c.Params("id"))
	if err != nil {
		return errorResponse(c, 409, err.Error())
	}
	return success(c, result)
}

func (h *Handler) adminMonitoring(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	round, parseErr := strconv.Atoi(c.Query("round", "0"))
	if parseErr != nil || round < 0 {
		return errorResponse(c, 422, "nomor putaran tidak valid")
	}
	result, err := h.service.MonitoringAtRound(c.UserContext(), c.Params("id"), c.Query("subjectCode"), round)
	if err != nil {
		return errorResponse(c, 404, err.Error())
	}
	return success(c, result)
}

func (h *Handler) picMonitoring(c *fiber.Ctx) error {
	claims := getClaims(c)
	if (c.Query("projectId") != "" && c.Query("projectId") != claims.ProjectID) || (c.Query("subjectCode") != "" && c.Query("subjectCode") != claims.SubjectCode) {
		return errorResponse(c, 403, "PIC hanya dapat memantau proyek dan mapel penugasannya")
	}
	if err := h.requireService(c); err != nil {
		return err
	}
	projectID, subjectCode := claims.ProjectID, claims.SubjectCode
	if c.Query("projectId") != "" {
		projectID = c.Query("projectId")
	}
	if c.Query("subjectCode") != "" {
		subjectCode = c.Query("subjectCode")
	}
	result, err := h.service.Monitoring(c.UserContext(), projectID, subjectCode)
	if err != nil {
		return errorResponse(c, 404, err.Error())
	}
	return success(c, result)
}

func (h *Handler) previewPanelist(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	claims := getClaims(c)
	round, parseErr := strconv.Atoi(c.Query("round", "1"))
	if parseErr != nil || round < 1 {
		return errorResponse(c, 422, "nomor putaran harus positif")
	}
	result, err := h.service.PreviewPanelist(c.UserContext(), claims.ProjectID, claims.SubjectCode, round, c.Params("panelistId"))
	if err != nil {
		return errorResponse(c, 403, err.Error())
	}
	return success(c, result)
}

func (h *Handler) roundAction(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	claims := getClaims(c)
	var request struct {
		Confirmation  string `json:"confirmation"`
		RoundNumber   int    `json:"roundNumber"`
		RoundRevision int    `json:"roundRevision"`
	}
	if err := c.BodyParser(&request); err != nil || request.RoundNumber < 1 {
		return errorResponse(c, 422, "nomor putaran wajib diisi")
	}
	round, err := h.service.RoundAction(c.UserContext(), claims.ProjectID, claims.SubjectCode, c.Params("action"), request.Confirmation, claims.Subject, request.RoundNumber, request.RoundRevision)
	if err != nil {
		return errorResponse(c, 409, err.Error())
	}
	return success(c, round)
}

func (h *Handler) itemDetail(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	item, err := h.service.GetItem(c.UserContext(), c.Params("id"))
	if err != nil {
		return errorResponse(c, 404, "ID butir tidak ditemukan")
	}
	return success(c, adminItem(item))
}

func (h *Handler) adminPreviewPanelist(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	round, err := strconv.Atoi(c.Query("round", "1"))
	if err != nil || round < 1 {
		return errorResponse(c, 422, "nomor putaran harus positif")
	}
	rows, err := h.service.PreviewPanelist(c.UserContext(), c.Params("id"), c.Query("subjectCode"), round, c.Params("panelistId"))
	if err != nil {
		return errorResponse(c, 403, err.Error())
	}
	return success(c, rows)
}

func (h *Handler) panelistForm(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	claims := getClaims(c)
	result, err := h.service.PanelistForm(c.UserContext(), claims.Subject, claims.ProjectID, claims.SubjectCode)
	if err != nil {
		return coreError(c, err, 404)
	}
	return success(c, panelistFormView(result))
}

func (h *Handler) saveSubmission(c *fiber.Ctx) error {
	if err := h.requireService(c); err != nil {
		return err
	}
	claims := getClaims(c)
	var request struct {
		service.SubmissionOptions
		RoundNumber int           `json:"roundNumber"`
		Answer      domain.Answer `json:"answer"`
	}
	if err := c.BodyParser(&request); err != nil {
		return errorResponse(c, 422, "jawaban tidak valid")
	}
	result, err := h.service.SaveSubmissionWithOptions(c.UserContext(), claims.Subject, claims.Name, claims.ProjectID, claims.SubjectCode, request.RoundNumber, c.Params("itemId"), request.Answer, request.SubmissionOptions)
	if err != nil {
		return coreError(c, err, 422)
	}
	return success(c, result)
}
