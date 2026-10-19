package domain

import "time"

// Evaluation stores one optimistic-concurrency controlled response per instrument.
// The deterministic ID enforces uniqueness without startup index dependencies.
type Evaluation struct {
	ID                string    `json:"id" bson:"_id"`
	ProjectID         string    `json:"projectId" bson:"projectId"`
	UserID            string    `json:"-" bson:"userId"`
	SubjectCode       string    `json:"subjectCode" bson:"subjectCode"`
	InstrumentVersion int       `json:"instrumentVersion" bson:"instrumentVersion"`
	Version           int       `json:"version" bson:"version"`
	Confidence        int       `json:"confidence" bson:"confidence"`
	Training          int       `json:"training" bson:"training"`
	TimeAdequacy      int       `json:"timeAdequacy" bson:"timeAdequacy"`
	Comment           string    `json:"comment" bson:"comment"`
	CreatedAt         time.Time `json:"createdAt" bson:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt" bson:"updatedAt"`
}
