package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"standard-setting/backend/internal/domain"
)

// Project subjects are the membership relation. Item.projectId is only a
// compatibility summary and must never transfer another project's membership.
// The caller runs synchronization and SaveProject in the same transaction.
func (m *Mongo) SyncProjectItems(ctx context.Context, projectID, projectName string, selectedIDs []string) error {
	selected, retained, affected := map[string]bool{}, map[string]bool{}, map[string]bool{}
	old, err := m.GetProject(ctx, projectID)
	if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		return err
	}
	for _, subject := range old.Subjects {
		for _, id := range subject.ItemIDs {
			retained[id] = true
			affected[id] = true
		}
	}
	// Include legacy summaries even when a project's old subject list is missing.
	cursor, err := m.items.Find(ctx, bson.M{"projectId": projectID})
	if err != nil {
		return err
	}
	var legacy []domain.Item
	err = cursor.All(ctx, &legacy)
	cursor.Close(ctx)
	if err != nil {
		return err
	}
	for _, item := range legacy {
		affected[item.ID] = true
	}
	for _, id := range selectedIDs {
		selected[id] = true
		affected[id] = true
	}
	// Sorting makes concurrent overlapping allocations acquire item writes in the same order.
	ids := make([]string, 0, len(affected))
	for id := range affected {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := m.reconcileItemAllocation(ctx, id, projectID, projectName, selected[id], retained[id]); err != nil {
			return err
		}
	}
	return nil
}

func (m *Mongo) reconcileItemAllocation(ctx context.Context, itemID, projectID, projectName string, selected, retained bool) error {
	item, err := m.GetItem(ctx, itemID)
	if err != nil {
		return err
	}
	if selected && !retained && (item.UsageStatus == domain.ItemTested || item.ReadOnly) {
		return fmt.Errorf("ID butir %s sudah diuji atau terkunci", itemID)
	}
	cursor, err := m.projects.Find(ctx, bson.M{
		"_id":              bson.M{"$ne": projectID},
		"subjects.itemIds": itemID,
	}, options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return err
	}
	var projects []domain.Project
	err = cursor.All(ctx, &projects)
	cursor.Close(ctx)
	if err != nil {
		return err
	}
	usages := make([]domain.ItemProjectUsage, 0, len(projects)+1)
	for _, p := range projects {
		usages = append(usages, domain.ItemProjectUsage{ProjectID: p.ID, ProjectName: p.Name})
	}
	if selected {
		usages = append(usages, domain.ItemProjectUsage{ProjectID: projectID, ProjectName: projectName})
	}
	primary := domain.ItemProjectUsage{}
	if len(usages) > 0 {
		primary = usages[0]
		for _, usage := range usages {
			if usage.ProjectID == item.ProjectID {
				primary = usage
				break
			}
		}
	}
	history := append([]domain.ItemProjectUsage{}, item.ProjectHistory...)
	for _, usage := range append([]domain.ItemProjectUsage{{ProjectID: item.ProjectID, ProjectName: item.ProjectName}}, usages...) {
		if usage.ProjectID == "" {
			continue
		}
		found := false
		for i := range history {
			if history[i].ProjectID == usage.ProjectID {
				history[i].ProjectName = usage.ProjectName
				found = true
				break
			}
		}
		if !found {
			history = append(history, usage)
		}
	}
	status := item.UsageStatus
	if status != domain.ItemTested {
		status = domain.ItemAvailable
		if len(usages) > 0 {
			status = domain.ItemAllocated
		}
	}
	_, err = m.items.UpdateOne(ctx, bson.M{"_id": itemID}, bson.M{"$set": bson.M{
		"projectId": primary.ProjectID, "projectName": primary.ProjectName, "projectHistory": history,
		"usageStatus": status, "updatedAt": time.Now().UTC(),
	}})
	return err
}

func (m *Mongo) MarkProjectItemsTested(ctx context.Context, projectID string) error {
	items, err := m.ListItems(ctx, "", projectID, false)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	_, err = m.items.UpdateMany(ctx, bson.M{"_id": bson.M{"$in": ids}}, bson.M{"$set": bson.M{"usageStatus": domain.ItemTested, "readOnly": true, "updatedAt": time.Now().UTC()}})
	return err
}

// SaveProjectItemResults changes only result fields. It allows another already
// assigned project to complete a tested item without unlocking bank metadata.
func (m *Mongo) SaveProjectItemResults(ctx context.Context, projectID, projectName string, items []domain.Item) error {
	project, err := m.GetProject(ctx, projectID)
	if err != nil {
		return err
	}
	selected := map[string]bool{}
	for _, subject := range project.Subjects {
		for _, id := range subject.ItemIDs {
			selected[id] = true
		}
	}
	for _, item := range items {
		if !selected[item.ID] {
			return fmt.Errorf("ID butir %s bukan bagian alokasi proyek", item.ID)
		}
		result, err := m.items.UpdateOne(ctx, bson.M{"_id": item.ID}, bson.M{"$set": bson.M{
			"finalResult": item.FinalResult, "usageStatus": domain.ItemTested, "readOnly": true,
			"projectId": projectID, "projectName": projectName, "updatedAt": time.Now().UTC(),
		}})
		if err != nil {
			return err
		}
		if result.MatchedCount != 1 {
			return mongo.ErrNoDocuments
		}
	}
	return nil
}
