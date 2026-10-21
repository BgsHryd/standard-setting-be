import {test} from 'node:test';
import assert from 'node:assert/strict';
import {discoverRoutes} from './verify_docs.mjs';

test('discovers route helpers across production files with nested profile scope', () => {
  const routes = discoverRoutes(new Map([
    ['router.go', `
      api := app.Group("/api/v1")
      protected := api.Group("")
      admin := protected.Group("")
      panelist := protected.Group("/panelist")
      pic := protected.Group("/pic")
      api.Post("/auth/login", h.login)
      panelist.Get("/form", h.form)
    `],
    ['features.go', `
      profile := protected.Group("/profile", h.role)
      profile.Get("/contact", h.contact)
      profile.Put("/contact", h.update)
      pic.Post("/rounds/:id/exclusions/:userId", h.exclude)
    `],
    ['report.go', `
      admin.Get("/projects/:id/reports", adminRole, h.report)
      panelist.Put("/evaluation", h.evaluation)
    `],
    ['security.go', `
      admin.Get("/audit-events", adminRole, h.audit)
      c.Get("Authorization")
      // admin.Get("/obsolete", h.removed)
    `],
    ['router_test.go', `
      api.Get("/test-only", h.fake)
    `]
  ]));
  assert.deepEqual([...routes].sort(), [
    'post /auth/login', 'get /panelist/form', 'get /profile/contact', 'put /profile/contact',
    'post /pic/rounds/{id}/exclusions/{userId}', 'get /projects/{id}/reports',
    'put /panelist/evaluation', 'get /audit-events'
  ].sort());
});

test('resolves groups independently of file order', () => {
  const routes = discoverRoutes(new Map([
    ['features.go', '  profile := protected.Group("/profile")\n  profile.Get("/contact", h.contact)'],
    ['router.go', '  protected := api.Group("")\n  api := app.Group("/api/v1")']
  ]));
  assert.deepEqual([...routes], ['get /profile/contact']);
});
