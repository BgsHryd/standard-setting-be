package domain

import (
	"context"
	"time"
)

// AuditEvent deliberately contains identifiers and enumerated activity, never credentials or free-form payloads.
type AuditEvent struct {
	ID        string    `json:"id" bson:"_id"`
	ActorID   string    `json:"actorId" bson:"actorId"`
	TargetID  string    `json:"targetId" bson:"targetId"`
	ProjectID string    `json:"projectId" bson:"projectId"`
	Action    string    `json:"action" bson:"action"`
	Outcome   string    `json:"outcome" bson:"outcome"`
	RequestID string    `json:"requestId" bson:"requestId"`
	CreatedAt time.Time `json:"createdAt" bson:"createdAt"`
}
type auditRequestKey struct{}

func WithAuditRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, auditRequestKey{}, id)
}
func AuditRequestID(ctx context.Context) string {
	id, _ := ctx.Value(auditRequestKey{}).(string)
	return id
}
