package domain

import "time"

type ImpactBinding struct {
	ProjectID      string                  `json:"projectId" bson:"projectId"`
	ProjectVersion int                     `json:"projectVersion" bson:"projectVersion"`
	FileHash       string                  `json:"fileHash" bson:"fileHash"`
	Snapshots      []ImpactSnapshotBinding `json:"snapshots" bson:"snapshots"`
}
type ImpactSnapshotBinding struct {
	SubjectCode string `json:"subjectCode" bson:"subjectCode"`
	SnapshotID  string `json:"snapshotId" bson:"snapshotId"`
	RoundNumber int    `json:"roundNumber" bson:"roundNumber"`
	Revision    int    `json:"revision" bson:"revision"`
}
type ImpactScore struct {
	ParticipantID string  `json:"participantId" bson:"participantId"`
	SubjectCode   string  `json:"subjectCode" bson:"subjectCode"`
	Score         float64 `json:"score" bson:"score"`
	Scale         string  `json:"scale" bson:"scale"`
	Maximum       float64 `json:"maximum" bson:"maximum"`
	Percentage    float64 `json:"percentage" bson:"percentage"`
	Passed        bool    `json:"passed" bson:"passed"`
}
type ImpactSubject struct {
	SubjectCode       string  `json:"subjectCode" bson:"subjectCode"`
	CutoffSnapshotID  string  `json:"cutoffSnapshotId" bson:"cutoffSnapshotId"`
	RoundingPolicy    string  `json:"roundingPolicy" bson:"roundingPolicy"`
	Cutoff            float64 `json:"cutoff" bson:"cutoff"`
	OperationalCutoff float64 `json:"operationalCutoff" bson:"operationalCutoff"`
	Count             int     `json:"count" bson:"count"`
	Passed            int     `json:"passed" bson:"passed"`
	Failed            int     `json:"failed" bson:"failed"`
	Percentage        float64 `json:"percentage" bson:"percentage"`
	Distribution      []int   `json:"distribution" bson:"distribution"`
	Stale             bool    `json:"stale" bson:"-"`
}
type ImpactBatch struct {
	ID        string          `json:"id" bson:"_id"`
	ProjectID string          `json:"projectId" bson:"projectId"`
	Binding   ImpactBinding   `json:"binding" bson:"binding"`
	Subjects  []ImpactSubject `json:"subjects" bson:"subjects"`
	Scores    []ImpactScore   `json:"scores" bson:"scores"`
	CreatedBy string          `json:"createdBy" bson:"createdBy"`
	CreatedAt time.Time       `json:"createdAt" bson:"createdAt"`
}
