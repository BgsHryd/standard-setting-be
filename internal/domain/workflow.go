package domain

import "time"

// RoundReceipt binds a deliberate finish action to the exact saved answer set.
type RoundReceipt struct {
	PanelistID       string         `json:"panelistId" bson:"panelistId"`
	RoundRevision    int            `json:"roundRevision" bson:"roundRevision"`
	CandidateVersion int            `json:"candidateVersion" bson:"candidateVersion"`
	Versions         map[string]int `json:"versions" bson:"versions"`
	SubmittedAt      *time.Time     `json:"submittedAt" bson:"submittedAt"`
}

type NBKDecision struct {
	SnapshotID  string     `json:"snapshotId" bson:"snapshotId"`
	RoundNumber int        `json:"roundNumber" bson:"roundNumber"`
	Revision    int        `json:"revision" bson:"revision"`
	Statistics  ItemResult `json:"statistics" bson:"statistics"`
	ActorID     string     `json:"actorId" bson:"actorId"`
	Reason      string     `json:"reason" bson:"reason"`
	LockedAt    time.Time  `json:"lockedAt" bson:"lockedAt"`
}

type PracticeSession struct {
	ID          string            `json:"-" bson:"_id"`
	UserID      string            `json:"-" bson:"userId"`
	ProjectID   string            `json:"-" bson:"projectId"`
	SubjectCode string            `json:"-" bson:"subjectCode"`
	Version     int               `json:"version" bson:"version"`
	StartedAt   time.Time         `json:"startedAt" bson:"startedAt"`
	FinishedAt  *time.Time        `json:"finishedAt" bson:"finishedAt"`
	Answers     map[string]Answer `json:"answers" bson:"answers"`
}
