package domain

import "strings"

type Subject struct {
	Code           string `json:"code" bson:"_id"`
	Name           string `json:"name" bson:"name"`
	NormalizedName string `json:"-" bson:"normalizedName"`
	Total          int    `json:"total" bson:"-"`
	Available      int    `json:"available" bson:"-"`
	Allocated      int    `json:"allocated" bson:"-"`
	Tested         int    `json:"tested" bson:"-"`
}

func NormalizeSubjectName(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

type PersonnelSlot struct {
	ID     string `json:"id" bson:"id"`
	Role   Role   `json:"role" bson:"role"`
	Name   string `json:"name" bson:"name"`
	UserID string `json:"userId,omitempty" bson:"userId,omitempty"`
}
type ItemProjectUsage struct {
	ProjectID   string `json:"projectId" bson:"projectId"`
	ProjectName string `json:"projectName" bson:"projectName"`
}
