// QA fixture for between-round results. This program only accepts its own scratch database prefix.
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
	"standard-setting/backend/internal/service"
	"standard-setting/backend/internal/store"
)

func main() {
	expire := flag.Bool("expire", false, "expire only this scratch project")
	cleanup := flag.Bool("cleanup", false, "remove this scratch database")
	flag.Parse()
	database := os.Getenv("QA_BETWEEN_DATABASE")
	if !strings.HasPrefix(database, "standard-setting-between-qa-") {
		log.Fatal("QA_BETWEEN_DATABASE must use the isolated standard-setting-between-qa- prefix")
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
	if *expire {
		p, err := st.GetProject(ctx, database+"-project")
		if err != nil {
			log.Fatal(err)
		}
		p.EndDate = time.Now().UTC().Add(-time.Minute)
		if err := st.SaveProject(ctx, p); err != nil {
			log.Fatal(err)
		}
		log.Print("Scratch project period expired")
		return
	}
	hash, err := auth.HashPassword("QA-between-password*")
	if err != nil {
		log.Fatal(err)
	}
	now := time.Now().UTC()
	projectID, picID, panelID := database+"-project", database+"-pic", database+"-panel"
	p := domain.Project{ID: projectID, Code: "SS-BETWEEN-QA", Name: "QA hasil dan diskusi antarputaran", Version: 1,
		FeatureVersion: 1, WorkflowVersion: 1, PlannedRounds: 3, WizardStep: 5, AccountsGenerated: true,
		Status: domain.ProjectActive, StartDate: now.Add(-time.Hour), EndDate: now.Add(24 * time.Hour), CreatedAt: now, UpdatedAt: now,
		Subjects: []domain.ProjectSubject{{Code: "MAT", Name: "Matematika", PICUserID: picID, PICName: "PIC Diskusi QA", PanelistCount: 3,
			PanelistNames: []string{"Panelis Diskusi QA", "Panelis Kedua QA", "Panelis Ketiga QA"}, PanelistUserIDs: []string{panelID, panelID + "-2", panelID + "-3"}, CandidateVersion: 1,
			CandidateDefinition: "Memahami konsep dasar dan menyelesaikan masalah rutin.", ItemIDs: []string{"BETWEEN-001", "BETWEEN-002", "BETWEEN-003"}}}}
	if err := st.SaveProject(ctx, p); err != nil {
		log.Fatal(err)
	}
	users := []domain.User{
		{ID: picID, Username: "qa.between.pic", Name: "PIC Diskusi QA", Role: domain.RolePICMapel},
		{ID: panelID + "-2", Username: "qa.between.panel2", Name: "Panelis Kedua QA", Role: domain.RolePanelist},
		{ID: panelID + "-3", Username: "qa.between.panel3", Name: "Panelis Ketiga QA", Role: domain.RolePanelist},
		{ID: panelID, Username: "qa.between.panel", Name: "Panelis Diskusi QA", Role: domain.RolePanelist},
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
		{ID: "BETWEEN-001", QuestionType: domain.QuestionTypePG, MaxScore: 1, KJ: "A,C", Measure: &zero},
		{ID: "BETWEEN-002", QuestionType: domain.QuestionTypeComplex, MaxScore: 3, KJ: "0", Measure: &negative},
		{ID: "BETWEEN-003", QuestionType: domain.QuestionTypeEssay, MaxScore: 4, KJ: strings.Repeat("KunciEsai", 18)},
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
	core := service.New(st, nil, "QA-between-password*")
	for i, id := range p.Subjects[0].PanelistUserIDs {
		value := []int{45, 70, 90}[i]
		distribution := make([]int, 5)
		distribution[i*2] = 100
		answers := []domain.Answer{
			{QuestionType: domain.QuestionTypePG, Value: &value},
			{QuestionType: domain.QuestionTypeComplex, Distribution: []int{0, 0, 100, 0}},
			{QuestionType: domain.QuestionTypeEssay, Distribution: distribution},
		}
		for j, answer := range answers {
			if _, err := core.SaveSubmissionWithOptions(ctx, id, "", projectID, "MAT", 1, items[j].ID, answer, service.SubmissionOptions{RoundRevision: 1, CandidateVersion: 1, Reviewed: true}); err != nil {
				log.Fatal(err)
			}
		}
		if _, err := core.SubmitPanelistRound(ctx, id, 1, 1, true); err != nil {
			log.Fatal(err)
		}
	}
	if _, err := core.RoundAction(ctx, projectID, "MAT", "finalize", "FINALISASI PUTARAN", picID, 1, 1); err != nil {
		log.Fatal(err)
	}
	log.Print("Isolated between-round QA fixture ready")
}
