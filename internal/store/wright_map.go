package store

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/gridfs"
	"go.mongodb.org/mongo-driver/mongo/options"
	"standard-setting/backend/internal/domain"
)

type wrightMapReference struct {
	ID        string             `bson:"_id"`
	FileID    primitive.ObjectID `bson:"fileId"`
	FileName  string             `bson:"fileName"`
	UpdatedAt time.Time          `bson:"updatedAt"`
	Version   int                `bson:"version"`
}

// The length prefix makes this unambiguous even when identifiers contain colons.
func wrightMapKey(project, subject string) string {
	return strconv.Itoa(len(project)) + ":" + project + ":" + subject
}

func (m *Mongo) GetWrightMap(ctx context.Context, project, subject string) (*domain.WrightMap, error) {
	bucket, err := gridfs.NewBucket(m.database, options.GridFSBucket().SetName("wright_maps"))
	if err != nil {
		return nil, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		var ref wrightMapReference
		err = m.database.Collection("wright_map_current").FindOne(ctx, bson.M{"_id": wrightMapKey(project, subject)}).Decode(&ref)
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		stream, e := bucket.OpenDownloadStream(ref.FileID)
		if e != nil {
			if errors.Is(e, gridfs.ErrFileNotFound) {
				continue
			}
			return nil, e
		}
		stream.SetReadDeadline(time.Now().Add(30 * time.Second))
		var buf bytes.Buffer
		_, err = buf.ReadFrom(stream)
		closeErr := stream.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
		return &domain.WrightMap{FileName: ref.FileName, Text: buf.String(), Version: ref.Version, UpdatedAt: ref.UpdatedAt}, nil
	}
	return nil, errors.New("Wright map sedang berubah; coba muat ulang")
}

func (m *Mongo) ReplaceWrightMap(ctx context.Context, project, subject string, expected int, value domain.WrightMap, authorize func(context.Context) error) (*domain.WrightMap, error) {
	bucket, err := gridfs.NewBucket(m.database, options.GridFSBucket().SetName("wright_maps"))
	if err != nil {
		return nil, err
	}
	// GridFS does not support multi-document transactions. Stage the immutable blob,
	// then atomically switch the single current pointer in the project transaction.
	id := primitive.NewObjectID()
	stream, err := bucket.OpenUploadStreamWithID(id, value.FileName, options.GridFSUpload().SetMetadata(bson.M{"projectId": project, "subjectCode": subject}))
	if err != nil {
		return nil, err
	}
	stream.SetWriteDeadline(time.Now().Add(30 * time.Second))
	if _, err = stream.Write([]byte(value.Text)); err != nil {
		_ = stream.Abort()
		return nil, err
	}
	if err = stream.Close(); err != nil {
		_ = bucket.Delete(id)
		return nil, err
	}
	key := wrightMapKey(project, subject)
	var previous wrightMapReference
	err = m.WithTransaction(ctx, func(tx context.Context) error {
		if e := authorize(tx); e != nil {
			return e
		}
		previous = wrightMapReference{}
		e := m.database.Collection("wright_map_current").FindOne(tx, bson.M{"_id": key}).Decode(&previous)
		if e != nil && !errors.Is(e, mongo.ErrNoDocuments) {
			return e
		}
		if previous.Version != expected {
			return domain.ErrWrightMapConflict
		}
		ref := wrightMapReference{ID: key, FileID: id, FileName: value.FileName, UpdatedAt: value.UpdatedAt, Version: expected + 1}
		if previous.Version == 0 {
			_, e = m.database.Collection("wright_map_current").InsertOne(tx, ref)
		} else {
			var res *mongo.UpdateResult
			res, e = m.database.Collection("wright_map_current").ReplaceOne(tx, bson.M{"_id": key, "version": expected}, ref)
			if e == nil && res.MatchedCount != 1 {
				return domain.ErrWrightMapConflict
			}
		}
		if mongo.IsDuplicateKeyError(e) {
			return domain.ErrWrightMapConflict
		}
		return e
	})
	if err != nil {
		_ = bucket.Delete(id)
		return nil, err
	}
	if !previous.FileID.IsZero() {
		_ = bucket.Delete(previous.FileID)
	}
	value.Version = expected + 1
	return &value, nil
}
