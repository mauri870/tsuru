// Copyright 2017 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mongodb

import (
	"context"

	"github.com/globalsign/mgo"
	"github.com/globalsign/mgo/bson"
	"github.com/tsuru/tsuru/db"
	dbStorage "github.com/tsuru/tsuru/db/storage"
	"github.com/tsuru/tsuru/db/storagev2"
	"github.com/tsuru/tsuru/types/app"
	mongoBSON "go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const plansCollectionName = "plans"

var _ app.PlanStorage = &PlanStorage{}

type PlanStorage struct{}

type planOnMongoDB struct {
	Name     string `bson:"_id"`
	Memory   int64
	CPUMilli int
	CPUBurst *app.CPUBurst
	Default  bool
	Override *app.PlanOverride `bson:"-"`
}

func plansCollection(conn *db.Storage) *dbStorage.Collection {
	return conn.Collection(plansCollectionName)
}

func (s *PlanStorage) Insert(ctx context.Context, p app.Plan) error {
	conn, err := db.Conn()
	if err != nil {
		return err
	}
	defer conn.Close()
	if p.Default {
		query := bson.M{"default": true}
		span := newMongoDBSpan(ctx, mongoSpanUpdateAll, plansCollectionName)
		span.SetQueryStatement(query)
		defer span.Finish()

		_, err = plansCollection(conn).UpdateAll(query, bson.M{"$unset": bson.M{"default": false}})
		if err != nil {
			span.SetError(err)
			return err
		}
	}

	span := newMongoDBSpan(ctx, mongoSpanInsert, plansCollectionName)
	defer span.Finish()

	err = plansCollection(conn).Insert(planOnMongoDB(p))
	if err != nil && mgo.IsDup(err) {
		return app.ErrPlanAlreadyExists
	}
	span.SetError(err)
	return err
}

func (s *PlanStorage) FindAll(ctx context.Context) ([]app.Plan, error) {
	return s.findByQuery(ctx, mongoBSON.M{})
}

func (s *PlanStorage) FindDefault(ctx context.Context) (*app.Plan, error) {
	plans, err := s.findByQuery(ctx, mongoBSON.M{"default": true})
	if err != nil {
		return nil, err
	}
	if len(plans) > 1 {
		return nil, app.ErrPlanDefaultAmbiguous
	}
	if len(plans) == 0 {
		return nil, app.ErrPlanDefaultNotFound
	}
	return &plans[0], nil
}

func (s *PlanStorage) findByQuery(ctx context.Context, query mongoBSON.M) ([]app.Plan, error) {
	span := newMongoDBSpan(ctx, mongoSpanFind, plansCollectionName)
	span.SetQueryStatement(query)
	defer span.Finish()

	collection, err := storagev2.Collection(plansCollectionName)
	if err != nil {
		span.SetError(err)
		return nil, err
	}

	cursor, err := collection.Find(ctx, query, &options.FindOptions{Sort: mongoBSON.M{"_id": 1}})
	if err != nil {
		span.SetError(err)
		return nil, err
	}

	plans := []planOnMongoDB{}
	err = cursor.All(ctx, &plans)
	if err != nil {
		span.SetError(err)
		return nil, err
	}

	appPlans := make([]app.Plan, len(plans))
	for i, p := range plans {
		appPlans[i] = app.Plan(p)
	}
	return appPlans, nil
}

func (s *PlanStorage) FindByName(ctx context.Context, name string) (*app.Plan, error) {
	span := newMongoDBSpan(ctx, mongoSpanFind, plansCollectionName)
	span.SetMongoID(name)
	defer span.Finish()

	var p planOnMongoDB
	collection, err := storagev2.Collection(plansCollectionName)
	if err != nil {
		span.SetError(err)
		return nil, err
	}
	err = collection.FindOne(ctx, mongoBSON.M{"_id": name}).Decode(&p)
	if err != nil {
		span.SetError(err)
		if err == mongo.ErrNoDocuments {
			err = app.ErrPlanNotFound
		}
		return nil, err
	}
	plan := app.Plan(p)
	return &plan, nil
}

func (s *PlanStorage) Delete(ctx context.Context, p app.Plan) error {
	span := newMongoDBSpan(ctx, mongoSpanDelete, plansCollectionName)
	span.SetMongoID(p.Name)
	defer span.Finish()

	conn, err := db.Conn()
	if err != nil {
		span.SetError(err)
		return err
	}
	defer conn.Close()
	err = plansCollection(conn).RemoveId(p.Name)
	if err == mgo.ErrNotFound {
		return app.ErrPlanNotFound
	}
	span.SetError(err)
	return err
}
