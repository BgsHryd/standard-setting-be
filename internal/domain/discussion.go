package domain

import "time"

// DiscussionNote is independent of grading so discussion cannot mark an item done.
type DiscussionNote struct {
	ID            string    `json:"-" bson:"_id"`
	UserID        string    `json:"-" bson:"userId"`
	ProjectID     string    `json:"-" bson:"projectId"`
	SubjectCode   string    `json:"-" bson:"subjectCode"`
	ItemID        string    `json:"itemId" bson:"itemId"`
	RoundNumber   int       `json:"roundNumber" bson:"roundNumber"`
	RoundRevision int       `json:"roundRevision" bson:"roundRevision"`
	Version       int       `json:"version" bson:"version"`
	Text          string    `json:"text" bson:"text"`
	UpdatedAt     time.Time `json:"updatedAt" bson:"updatedAt"`
}
