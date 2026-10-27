package domain

import "time"

// PanelistTelemetry contains browser observations, never verified network or human performance.
// Every record belongs to one real grading round revision; practice has no telemetry scope.
type PanelistTelemetry struct {
	ID                  string    `json:"-" bson:"_id"`
	UserID              string    `json:"userId" bson:"userId"`
	ProjectID           string    `json:"projectId" bson:"projectId"`
	SubjectCode         string    `json:"subjectCode" bson:"subjectCode"`
	RoundNumber         int       `json:"roundNumber" bson:"roundNumber"`
	RoundRevision       int       `json:"roundRevision" bson:"roundRevision"`
	State               string    `json:"state" bson:"state"`
	Failures            int       `json:"failures" bson:"failures"`
	MedianMs            float64   `json:"medianMs" bson:"medianMs"`
	TimingSamples       int       `json:"timingSamples" bson:"timingSamples"`
	OfflineSeconds      float64   `json:"offlineSeconds" bson:"offlineSeconds"`
	ConnectionWarning   bool      `json:"connectionWarning" bson:"connectionWarning"`
	AnswerTimingSamples int       `json:"answerTimingSamples" bson:"answerTimingSamples"`
	MedianAnswerSeconds float64   `json:"medianAnswerSeconds" bson:"medianAnswerSeconds"`
	SpeedWarning        bool      `json:"speedWarning" bson:"speedWarning"`
	LastActive          time.Time `json:"lastActive" bson:"lastActive"`
}
