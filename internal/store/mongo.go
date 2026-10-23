package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readconcern"
	"go.mongodb.org/mongo-driver/mongo/writeconcern"
	"standard-setting/backend/internal/domain"
)

type Mongo struct {
	client      *mongo.Client
	database    *mongo.Database
	users       *mongo.Collection
	projects    *mongo.Collection
	items       *mongo.Collection
	rounds      *mongo.Collection
	submissions *mongo.Collection
}

func NewMongo(ctx context.Context, uri, database string) (*Mongo, error) {
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}
	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(ctx)
		return nil, fmt.Errorf("ping mongodb: %w", err)
	}
	db := client.Database(database)
	result := &Mongo{
		client: client, database: db, users: db.Collection("users"), projects: db.Collection("projects"),
		items: db.Collection("items"), rounds: db.Collection("rounds"), submissions: db.Collection("submissions"),
	}
	if err := EnsureIndexes(ctx, db); err != nil {
		_ = client.Disconnect(ctx)
		return nil, err
	}
	if err := EnsureCatalog(ctx, db); err != nil {
		_ = client.Disconnect(ctx)
		return nil, err
	}
	return result, nil
}

func (m *Mongo) Close(ctx context.Context) error { return m.client.Disconnect(ctx) }

func (m *Mongo) WithTransaction(ctx context.Context, fn func(context.Context) error) error {
	if mongo.SessionFromContext(ctx) != nil {
		return fn(ctx)
	}
	session, err := m.client.StartSession()
	if err != nil {
		return err
	}
	defer session.EndSession(ctx)
	_, err = session.WithTransaction(ctx, func(sc mongo.SessionContext) (any, error) { return nil, fn(sc) },
		options.Transaction().SetReadConcern(readconcern.Snapshot()).SetWriteConcern(writeconcern.Majority()))
	var command mongo.CommandError
	if errors.As(err, &command) && command.Code == 20 {
		return errors.New("MongoDB perlu replica set agar perubahan data atomik; jalankan konfigurasi MongoDB pada docker-compose.yml")
	}
	return err
}

func (m *Mongo) UpdateLastLogin(ctx context.Context, id string, at time.Time) error {
	_, err := m.users.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"lastLoginAt": at, "updatedAt": at}})
	return err
}

func (m *Mongo) ReplaceProjectUsers(ctx context.Context, projectID string, users []domain.User) error {
	ids := make([]string, 0, len(users))
	for _, user := range users {
		ids = append(ids, user.ID)
		if _, err := m.users.ReplaceOne(ctx, bson.M{"_id": user.ID}, user, options.Replace().SetUpsert(true)); err != nil {
			return err
		}
	}
	_, err := m.users.UpdateMany(ctx, bson.M{"projectId": projectID, "_id": bson.M{"$nin": ids}}, bson.M{"$set": bson.M{"active": false, "removed": true, "updatedAt": time.Now().UTC()}, "$inc": bson.M{"authVersion": 1}})
	return err
}

func (m *Mongo) FindByUsername(ctx context.Context, username string) (domain.User, error) {
	var user domain.User
	err := m.users.FindOne(ctx, bson.M{"username": username}, options.FindOne().SetCollation(&options.Collation{Locale: "en", Strength: 2})).Decode(&user)
	return user, err
}

func (m *Mongo) FindByID(ctx context.Context, id string) (domain.User, error) {
	var user domain.User
	err := m.users.FindOne(ctx, bson.M{"_id": id}).Decode(&user)
	return user, err
}

func (m *Mongo) UpdatePassword(ctx context.Context, id, passwordHash string, mustChange bool) error {
	result, err := m.users.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"passwordHash": passwordHash, "mustChangePassword": mustChange, "updatedAt": time.Now()}, "$inc": bson.M{"authVersion": 1}})
	if err == nil && result.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return err
}

func (m *Mongo) ListUsers(ctx context.Context, projectID string) ([]domain.User, error) {
	filter := bson.M{}
	if projectID != "" {
		filter["projectId"] = projectID
	}
	cursor, err := m.users.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	users := make([]domain.User, 0)
	if err := cursor.All(ctx, &users); err != nil {
		return nil, err
	}
	return users, nil
}

func (m *Mongo) InsertUsers(ctx context.Context, users []domain.User) error {
	if len(users) == 0 {
		return nil
	}
	documents := make([]any, len(users))
	for index := range users {
		documents[index] = users[index]
	}
	_, err := m.users.InsertMany(ctx, documents)
	return err
}

func (m *Mongo) ListProjects(ctx context.Context) ([]domain.Project, error) {
	cursor, err := m.projects.Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "updatedAt", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	projects := make([]domain.Project, 0)
	if err := cursor.All(ctx, &projects); err != nil {
		return nil, err
	}
	return projects, nil
}

func (m *Mongo) GetProject(ctx context.Context, id string) (domain.Project, error) {
	var project domain.Project
	err := m.projects.FindOne(ctx, bson.M{"_id": id}).Decode(&project)
	return project, err
}

func (m *Mongo) SaveProject(ctx context.Context, project domain.Project) error {
	_, err := m.projects.ReplaceOne(ctx, bson.M{"_id": project.ID}, project, options.Replace().SetUpsert(true))
	return err
}

func (m *Mongo) DeleteProject(ctx context.Context, id string) error {
	project, err := m.GetProject(ctx, id)
	if err != nil {
		return err
	}
	if project.Status != domain.ProjectDraft {
		return errors.New("hanya proyek draft yang dapat dihapus")
	}
	if _, err := m.users.UpdateMany(ctx, bson.M{"projectId": id}, bson.M{"$set": bson.M{"active": false, "removed": true, "updatedAt": time.Now().UTC()}, "$inc": bson.M{"authVersion": 1}}); err != nil {
		return err
	}
	if _, err := m.projects.DeleteOne(ctx, bson.M{"_id": id}); err != nil {
		return err
	}
	_, err = m.items.UpdateMany(ctx, bson.M{"projectId": id, "usageStatus": domain.ItemAllocated}, bson.M{"$set": bson.M{"projectId": "", "projectName": "", "usageStatus": domain.ItemAvailable, "updatedAt": time.Now()}})
	return err
}

func (m *Mongo) SyncProjectItems(ctx context.Context, projectID, projectName string, selectedIDs []string) error {
	if _, err := m.items.UpdateMany(ctx, bson.M{"projectHistory": nil}, bson.M{"$set": bson.M{"projectHistory": bson.A{}}}); err != nil {
		return err
	}
	if _, err := m.items.UpdateMany(ctx, bson.M{"projectId": projectID, "usageStatus": domain.ItemAllocated, "_id": bson.M{"$nin": selectedIDs}}, bson.M{"$set": bson.M{"projectId": "", "projectName": "", "usageStatus": domain.ItemAvailable, "updatedAt": time.Now()}}); err != nil {
		return err
	}
	for _, itemID := range selectedIDs {
		filter := bson.M{"_id": itemID, "usageStatus": bson.M{"$ne": domain.ItemTested}, "$or": bson.A{bson.M{"projectId": ""}, bson.M{"projectId": bson.M{"$exists": false}}, bson.M{"projectId": projectID}}}
		update := bson.M{"$set": bson.M{"projectId": projectID, "projectName": projectName, "usageStatus": domain.ItemAllocated, "updatedAt": time.Now()}, "$addToSet": bson.M{"projectHistory": domain.ItemProjectUsage{ProjectID: projectID, ProjectName: projectName}}}
		result, err := m.items.UpdateOne(ctx, filter, update)
		if err != nil {
			return err
		}
		if result.MatchedCount == 0 {
			return fmt.Errorf("ID butir %s sudah dialokasikan ke proyek lain atau sudah diuji", itemID)
		}
	}
	return nil
}

func (m *Mongo) MarkProjectItemsTested(ctx context.Context, projectID string) error {
	_, err := m.items.UpdateMany(ctx, bson.M{"projectId": projectID}, bson.M{"$set": bson.M{"usageStatus": domain.ItemTested, "readOnly": true, "updatedAt": time.Now()}})
	return err
}

func (m *Mongo) ListItems(ctx context.Context, subjectCode, projectID string, onlyAvailable bool) ([]domain.Item, error) {
	filter := bson.M{}
	if subjectCode != "" {
		filter["subjectCode"] = subjectCode
	}
	if projectID != "" {
		filter["projectId"] = projectID
	}
	if onlyAvailable {
		filter["usageStatus"] = domain.ItemAvailable
	}
	cursor, err := m.items.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	items := make([]domain.Item, 0)
	if err := cursor.All(ctx, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (m *Mongo) GetItem(ctx context.Context, id string) (domain.Item, error) {
	var item domain.Item
	err := m.items.FindOne(ctx, bson.M{"_id": id}).Decode(&item)
	return item, err
}

func (m *Mongo) UpsertItems(ctx context.Context, items []domain.Item) error {
	for _, item := range items {
		if item.ProjectHistory == nil {
			item.ProjectHistory = []domain.ItemProjectUsage{}
		}
		if _, err := m.items.ReplaceOne(ctx, bson.M{"_id": item.ID, "usageStatus": bson.M{"$ne": domain.ItemTested}}, item, options.Replace().SetUpsert(true)); err != nil {
			return err
		}
	}
	return nil
}

func (m *Mongo) CurrentRound(ctx context.Context, projectID, subjectCode string) (domain.Round, error) {
	var round domain.Round
	err := m.rounds.FindOne(ctx, bson.M{"projectId": projectID, "subjectCode": subjectCode, "status": "active"}, options.FindOne().SetSort(bson.D{{Key: "number", Value: -1}})).Decode(&round)
	return round, err
}

func (m *Mongo) SaveRound(ctx context.Context, round domain.Round) error {
	_, err := m.rounds.ReplaceOne(ctx, bson.M{"_id": round.ID}, round, options.Replace().SetUpsert(true))
	return err
}

func (m *Mongo) ListRounds(ctx context.Context, projectID, subjectCode string) ([]domain.Round, error) {
	cursor, err := m.rounds.Find(ctx, bson.M{"projectId": projectID, "subjectCode": subjectCode}, options.Find().SetSort(bson.D{{Key: "number", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	rounds := make([]domain.Round, 0)
	err = cursor.All(ctx, &rounds)
	return rounds, err
}

func (m *Mongo) ListSubmissions(ctx context.Context, projectID, subjectCode string, roundNumber int, panelistID string) ([]domain.Submission, error) {
	filter := submissionFilter(projectID, subjectCode, roundNumber, panelistID)
	cursor, err := m.submissions.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "updatedAt", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	submissions := make([]domain.Submission, 0)
	err = cursor.All(ctx, &submissions)
	return submissions, err
}

func submissionFilter(projectID, subjectCode string, roundNumber int, panelistID string) bson.M {
	filter := bson.M{"projectId": projectID}
	if subjectCode != "" {
		filter["subjectCode"] = subjectCode
	}
	if roundNumber > 0 {
		filter["roundNumber"] = roundNumber
	}
	if panelistID != "" {
		filter["panelistId"] = panelistID
	}
	return filter
}

func (m *Mongo) UpsertSubmission(ctx context.Context, submission domain.Submission) (domain.Submission, error) {
	filter := bson.M{"projectId": submission.ProjectID, "subjectCode": submission.SubjectCode, "roundNumber": submission.RoundNumber, "panelistId": submission.PanelistID, "itemId": submission.ItemID}
	encoded, err := bson.Marshal(submission)
	if err != nil {
		return domain.Submission{}, err
	}
	var fields bson.M
	if err := bson.Unmarshal(encoded, &fields); err != nil {
		return domain.Submission{}, err
	}
	delete(fields, "_id")
	var saved domain.Submission
	err = m.submissions.FindOneAndUpdate(ctx, filter, bson.M{"$set": fields, "$setOnInsert": bson.M{"_id": submission.ID}},
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)).Decode(&saved)
	return saved, err
}

func (m *Mongo) TouchRound(ctx context.Context, round domain.Round) error {
	result, err := m.rounds.UpdateOne(ctx, bson.M{"_id": round.ID, "status": "active", "number": round.Number}, bson.M{"$set": bson.M{"updatedAt": time.Now().UTC()}})
	if err == nil && result.MatchedCount != 1 {
		return errors.New("putaran sudah berubah")
	}
	return err
}

// Seed is retained for the explicit demo CLI. Normal startup calls BootstrapAdmin only.
func (m *Mongo) Seed(ctx context.Context, initialPasswordHash string) error {
 if err:=m.BootstrapAdmin(ctx,initialPasswordHash);err!=nil{return err}
 return m.SeedDemoItems(ctx)
}
func(m *Mongo) SeedDemoItems(ctx context.Context)error{
 now:=time.Now().UTC()

	subjects := []struct{ code, name string }{{"MAT", "Matematika"}, {"BIN", "Bahasa Indonesia"}, {"IPA", "Ilmu Pengetahuan Alam"}}
	types := []domain.QuestionType{domain.QuestionTypePG, domain.QuestionTypeComplex, domain.QuestionTypeEssay}
	for _, subject := range subjects {
		for index := 1; index <= 12; index++ {
			id := fmt.Sprintf("%s-%03d", subject.code, index)
			questionType := types[(index-1)%len(types)]
			maxScore := 1
			rubric := []string(nil)
			if questionType == domain.QuestionTypeComplex {
				maxScore = 3
				rubric = []string{"0", "1", "2", "3"}
			} else if questionType == domain.QuestionTypeEssay {
				maxScore = 4
				rubric = []string{"0", "1", "2", "3", "4"}
			}
			item := domain.Item{ID: id, SubjectCode: subject.code, SubjectName: subject.name, QuestionType: questionType, MaxScore: maxScore, Rubric: rubric, UsageStatus: domain.ItemAvailable, CreatedAt: now, UpdatedAt: now}
			if _, err := m.items.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$setOnInsert": item}, options.Update().SetUpsert(true)); err != nil {
				return err
			}
		}
	}
	return nil
}
