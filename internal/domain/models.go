package domain

import "time"

type Role string

const (
	RoleAdminSuper Role = "admin_super"
	RolePICMapel   Role = "pic_mapel"
	RolePanelist   Role = "panelis"
)

type ProjectStatus string

const (
	ProjectDraft     ProjectStatus = "draft"
	ProjectActive    ProjectStatus = "active"
	ProjectCompleted ProjectStatus = "completed"
)

type ItemUsageStatus string

const (
	ItemAvailable ItemUsageStatus = "available"
	ItemAllocated ItemUsageStatus = "allocated"
	ItemTested    ItemUsageStatus = "tested"
)

type QuestionType string

const (
	QuestionTypePG      QuestionType = "pilihan_ganda"
	QuestionTypeComplex QuestionType = "pilihan_ganda_kompleks"
	QuestionTypeEssay   QuestionType = "essay"
)

type User struct {
	SlotID             string     `json:"slotId,omitempty" bson:"slotId,omitempty"`
	Removed            bool       `json:"removed" bson:"removed"`
	AuthVersion        int        `json:"-" bson:"authVersion"`
	ID                 string     `json:"id" bson:"_id,omitempty"`
	Username           string     `json:"username" bson:"username"`
	Name               string     `json:"name" bson:"name"`
	PasswordHash       string     `json:"-" bson:"passwordHash"`
	Role               Role       `json:"role" bson:"role"`
	ProjectID          string     `json:"projectId,omitempty" bson:"projectId,omitempty"`
	ProjectName        string     `json:"projectName,omitempty" bson:"projectName,omitempty"`
	SubjectCode        string     `json:"subjectCode,omitempty" bson:"subjectCode,omitempty"`
	MustChangePassword bool       `json:"mustChangePassword" bson:"mustChangePassword"`
	Active             bool       `json:"active" bson:"active"`
	LastLoginAt        *time.Time `json:"lastLoginAt,omitempty" bson:"lastLoginAt,omitempty"`
	CreatedAt          time.Time  `json:"createdAt" bson:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt" bson:"updatedAt"`
}

type Project struct {
	FeatureVersion        int              `json:"featureVersion" bson:"featureVersion"`
	CollectContactPanelis bool             `json:"collectContactPanelis" bson:"collectContactPanelis"`
	RoundingPolicy        string           `json:"roundingPolicy" bson:"roundingPolicy"`
	SourceProjectID       string           `json:"sourceProjectId,omitempty" bson:"sourceProjectId,omitempty"`
	EvaluationClosed      bool             `json:"evaluationClosed" bson:"evaluationClosed"`
	Version               int              `json:"version" bson:"version"`
	ItemsEditable         bool             `json:"itemsEditable" bson:"-"`
	ID                    string           `json:"id" bson:"_id,omitempty"`
	Code                  string           `json:"code" bson:"code"`
	Name                  string           `json:"name" bson:"name"`
	Description           string           `json:"description,omitempty" bson:"description,omitempty"`
	StartDate             time.Time        `json:"startDate" bson:"startDate"`
	EndDate               time.Time        `json:"endDate" bson:"endDate"`
	Status                ProjectStatus    `json:"status" bson:"status"`
	WizardStep            int              `json:"wizardStep" bson:"wizardStep"`
	Subjects              []ProjectSubject `json:"subjects" bson:"subjects"`
	AccountsGenerated     bool             `json:"accountsGenerated" bson:"accountsGenerated"`
	CreatedBy             string           `json:"createdBy" bson:"createdBy"`
	CompletedAt           *time.Time       `json:"completedAt,omitempty" bson:"completedAt,omitempty"`
	CreatedAt             time.Time        `json:"createdAt" bson:"createdAt"`
	UpdatedAt             time.Time        `json:"updatedAt" bson:"updatedAt"`
}

type ProjectSubject struct {
	CandidateDefinition string          `json:"candidateDefinition" bson:"candidateDefinition"`
	CandidateVersion    int             `json:"candidateVersion" bson:"candidateVersion"`
	Slots               []PersonnelSlot `json:"slots" bson:"slots"`
	Code                string          `json:"code" bson:"code"`
	Name                string          `json:"name" bson:"name"`
	ItemIDs             []string        `json:"itemIds" bson:"itemIds"`
	PICUserID           string          `json:"picUserId,omitempty" bson:"picUserId,omitempty"`
	PICName             string          `json:"picName,omitempty" bson:"picName,omitempty"`
	PanelistCount       int             `json:"panelistCount" bson:"panelistCount"`
	PanelistUserIDs     []string        `json:"panelistUserIds,omitempty" bson:"panelistUserIds,omitempty"`
	PanelistNames       []string        `json:"panelistNames,omitempty" bson:"panelistNames,omitempty"`
}

type Item struct {
	ProjectHistory []ItemProjectUsage `json:"projectHistory" bson:"projectHistory"`
	ID             string             `json:"id" bson:"_id"`
	SubjectCode    string             `json:"subjectCode" bson:"subjectCode"`
	SubjectName    string             `json:"subjectName" bson:"subjectName"`
	QuestionType   QuestionType       `json:"questionType" bson:"questionType"`
	MaxScore       int                `json:"maxScore" bson:"maxScore"`
	Rubric         []string           `json:"rubric,omitempty" bson:"rubric,omitempty"`
	UsageStatus    ItemUsageStatus    `json:"usageStatus" bson:"usageStatus"`
	ProjectID      string             `json:"projectId,omitempty" bson:"projectId,omitempty"`
	ProjectName    string             `json:"projectName,omitempty" bson:"projectName,omitempty"`
	ReadOnly       bool               `json:"readOnly" bson:"readOnly"`
	FinalResult    *ItemResult        `json:"finalResult,omitempty" bson:"finalResult,omitempty"`
	CreatedAt      time.Time          `json:"createdAt" bson:"createdAt"`
	UpdatedAt      time.Time          `json:"updatedAt" bson:"updatedAt"`
}

type ItemResult struct {
	Revision                int       `json:"revision" bson:"revision"`
	SampleStandardDeviation *float64  `json:"sampleStandardDeviation" bson:"sampleStandardDeviation"`
	StandardError           *float64  `json:"standardError" bson:"standardError"`
	OperationalCutoff       float64   `json:"operationalCutoff" bson:"operationalCutoff"`
	RoundingPolicy          string    `json:"roundingPolicy" bson:"roundingPolicy"`
	PanelistIDs             []string  `json:"panelistIds,omitempty" bson:"panelistIds,omitempty"`
	RoundNumber             int       `json:"roundNumber" bson:"roundNumber"`
	Cutoff                  float64   `json:"cutoff" bson:"cutoff"`
	Mean                    float64   `json:"mean" bson:"mean"`
	Median                  float64   `json:"median" bson:"median"`
	StandardDev             float64   `json:"standardDeviation" bson:"standardDeviation"`
	Minimum                 float64   `json:"minimum" bson:"minimum"`
	Maximum                 float64   `json:"maximum" bson:"maximum"`
	PanelistCount           int       `json:"panelistCount" bson:"panelistCount"`
	CompletedAt             time.Time `json:"completedAt" bson:"completedAt"`
}

type Round struct {
	Revision          int              `json:"revision" bson:"revision"`
	ExcludedPanelists []RoundExclusion `json:"excludedPanelists" bson:"excludedPanelists"`
	FinalSnapshot     *RoundSnapshot   `json:"finalSnapshot,omitempty" bson:"finalSnapshot,omitempty"`
	Snapshots         []RoundSnapshot  `json:"snapshots,omitempty" bson:"snapshots,omitempty"`
	ID                string           `json:"id" bson:"_id,omitempty"`
	ProjectID         string           `json:"projectId" bson:"projectId"`
	SubjectCode       string           `json:"subjectCode" bson:"subjectCode"`
	Number            int              `json:"number" bson:"number"`
	Status            string           `json:"status" bson:"status"`
	EndsAt            *time.Time       `json:"endsAt,omitempty" bson:"endsAt,omitempty"`
	FinalizedAt       *time.Time       `json:"finalizedAt,omitempty" bson:"finalizedAt,omitempty"`
	FinalizedBy       string           `json:"finalizedBy,omitempty" bson:"finalizedBy,omitempty"`
	CreatedAt         time.Time        `json:"createdAt" bson:"createdAt"`
	UpdatedAt         time.Time        `json:"updatedAt" bson:"updatedAt"`
}

type Answer struct {
	QuestionType QuestionType `json:"questionType" bson:"questionType"`
	Value        *int         `json:"value,omitempty" bson:"value,omitempty"`
	Distribution []int        `json:"distribution,omitempty" bson:"distribution,omitempty"`
}

type Submission struct {
	Version          int       `json:"version" bson:"version"`
	RoundRevision    int       `json:"roundRevision" bson:"roundRevision"`
	ReviewRevision   int       `json:"reviewRevision" bson:"reviewRevision"`
	CandidateVersion int       `json:"candidateVersion" bson:"candidateVersion"`
	Note             string    `json:"note" bson:"note"`
	IdempotencyKey   string    `json:"idempotencyKey,omitempty" bson:"idempotencyKey,omitempty"`
	ID               string    `json:"id" bson:"_id,omitempty"`
	ProjectID        string    `json:"projectId" bson:"projectId"`
	SubjectCode      string    `json:"subjectCode" bson:"subjectCode"`
	RoundNumber      int       `json:"roundNumber" bson:"roundNumber"`
	PanelistID       string    `json:"panelistId" bson:"panelistId"`
	PanelistName     string    `json:"panelistName" bson:"panelistName"`
	ItemID           string    `json:"itemId" bson:"itemId"`
	Answer           Answer    `json:"answer" bson:"answer"`
	Status           string    `json:"status" bson:"status"`
	UpdatedAt        time.Time `json:"updatedAt" bson:"updatedAt"`
}

type RoundExclusion struct {
	PanelistID string    `json:"panelistId" bson:"panelistId"`
	Reason     string    `json:"reason" bson:"reason"`
	ActorID    string    `json:"actorId" bson:"actorId"`
	CreatedAt  time.Time `json:"createdAt" bson:"createdAt"`
}

type RoundSnapshot struct {
	ID                  string                `json:"id" bson:"id"`
	Revision            int                   `json:"revision" bson:"revision"`
	RoundNumber         int                   `json:"roundNumber" bson:"roundNumber"`
	SubjectCode         string                `json:"subjectCode" bson:"subjectCode"`
	PanelistIDs         []string              `json:"panelistIds" bson:"panelistIds"`
	Statistics          ItemResult            `json:"statistics" bson:"statistics"`
	ItemStatistics      map[string]ItemResult `json:"itemStatistics" bson:"itemStatistics"`
	Submissions         []Submission          `json:"submissions" bson:"submissions"`
	CandidateDefinition string                `json:"candidateDefinition" bson:"candidateDefinition"`
	CandidateVersion    int                   `json:"candidateVersion" bson:"candidateVersion"`
	RoundingPolicy      string                `json:"roundingPolicy" bson:"roundingPolicy"`
	CreatedAt           time.Time             `json:"createdAt" bson:"createdAt"`
}
