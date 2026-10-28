// QA fixture for display settings. This program only accepts its own scratch database prefix.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"standard-setting/backend/internal/auth"
	"standard-setting/backend/internal/domain"
	"standard-setting/backend/internal/store"
)

func main() {
	cleanup := flag.Bool("cleanup", false, "remove this scratch database")
	flag.Parse()
	database := os.Getenv("QA_DISPLAY_DATABASE")
	if !strings.HasPrefix(database, "standard-setting-display-qa-") {
		log.Fatal("QA_DISPLAY_DATABASE must use the isolated standard-setting-display-qa- prefix")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	uri := "mongodb://127.0.0.1:27018/?replicaSet=ssqa&directConnection=true"
	if *cleanup {
		client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
		if err != nil {
			log.Fatal(err)
		}
		defer client.Disconnect(ctx)
		if err := client.Database(database).Drop(ctx); err != nil {
			log.Fatal(err)
		}
		log.Print("Scratch QA database removed")
		return
	}
	st, err := store.NewMongo(ctx, uri, database)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close(ctx)
	hash, err := auth.HashPassword("QA-display-password*")
	if err != nil {
		log.Fatal(err)
	}
	now := time.Now().UTC()
	projectID, picID, panelID := database+"-project", database+"-pic", database+"-panel"
	p := domain.Project{ID: projectID, Code: "SS-DISPLAY-QA", Name: "QA konfigurasi tampilan panelis", Version: 1,
		FeatureVersion: 1, WorkflowVersion: 1, PlannedRounds: 2, WizardStep: 5, AccountsGenerated: true,
		Status: domain.ProjectActive, StartDate: now.Add(-time.Hour), EndDate: now.Add(24 * time.Hour), CreatedAt: now, UpdatedAt: now,
		Subjects: []domain.ProjectSubject{{Code: "MAT", Name: "Matematika", PICUserID: picID, PICName: "PIC Tampilan QA", PanelistCount: 1,
			PanelistNames: []string{"Panelis Tampilan QA"}, PanelistUserIDs: []string{panelID}, CandidateVersion: 1,
			CandidateDefinition: "Memahami konsep dasar dan menyelesaikan masalah rutin.", ItemIDs: []string{"DISPLAY-001", "DISPLAY-002", "DISPLAY-003"}}}}
	if err := st.SaveProject(ctx, p); err != nil {
		log.Fatal(err)
	}
	users := []domain.User{
		{ID: picID, Username: "qa.display.pic", Name: "PIC Tampilan QA", Role: domain.RolePICMapel},
		{ID: panelID, Username: "qa.display.panel", Name: "Panelis Tampilan QA", Role: domain.RolePanelist},
	}
	for i := range users {
		users[i].ProjectID, users[i].ProjectName, users[i].SubjectCode, users[i].PasswordHash = projectID, p.Name, "MAT", hash
		users[i].Active, users[i].CreatedAt, users[i].UpdatedAt = true, now, now
	}
	if err := st.InsertUsers(ctx, users); err != nil {
		log.Fatal(err)
	}
	zero, negative := 0.0, -1.25
	items := []domain.Item{
		{ID: "DISPLAY-001", QuestionType: domain.QuestionTypePG, MaxScore: 1, KJ: "A,C", Measure: &zero},
		{ID: "DISPLAY-002", QuestionType: domain.QuestionTypeComplex, MaxScore: 3, KJ: "0", Measure: &negative},
		{ID: "DISPLAY-003", QuestionType: domain.QuestionTypeEssay, MaxScore: 4, KJ: strings.Repeat("KunciEsai", 18)},
	}
	for i := range items {
		items[i].SubjectCode, items[i].SubjectName, items[i].ProjectID, items[i].ProjectName = "MAT", "Matematika", projectID, p.Name
		items[i].UsageStatus = domain.ItemAllocated
	}
	if err := st.UpsertItems(ctx, items); err != nil {
		log.Fatal(err)
	}
	if err := st.SaveRound(ctx, domain.Round{ID: database + "-round", ProjectID: projectID, SubjectCode: "MAT", Number: 1, Revision: 1, Status: "active", CreatedAt: now, UpdatedAt: now}); err != nil {
		log.Fatal(err)
	}
	log.Print("Isolated display QA fixture ready")
}
