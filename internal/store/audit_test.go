package store

import (
	"go.mongodb.org/mongo-driver/bson"
	"testing"
)

func TestAuditFilterTreatsSearchLiterallyAndScopesProject(t *testing.T) {
	f := auditFilter("p", "a.*", "auth.login")
	if f["projectId"] != "p" || f["action"] != "auth.login" {
		t.Fatalf("scope lost: %v", f)
	}
	or := f["$or"].(bson.A)
	first := or[0].(bson.M)["actorId"].(bson.M)
	if first["$regex"] != "a\\.\\*" {
		t.Fatalf("search regex injected: %v", f)
	}
}
