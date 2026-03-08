package mongo

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Client wraps the MongoDB driver client with helpers for diffing.
type Client struct {
	inner *mongo.Client
	uri   string
}

// Connect creates a new Client connected to the given MongoDB URI.
func Connect(ctx context.Context, uri string, timeout time.Duration) (*Client, error) {
	opts := options.Client().ApplyURI(uri).SetTimeout(timeout)
	client, err := mongo.Connect(opts)
	if err != nil {
		return nil, fmt.Errorf("could not connect to %s: %w", RedactURI(uri), err)
	}

	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(ctx)
		return nil, fmt.Errorf("could not connect to %s: %w", RedactURI(uri), err)
	}

	return &Client{inner: client, uri: uri}, nil
}

// Disconnect closes the connection.
func (c *Client) Disconnect(ctx context.Context) error {
	return c.inner.Disconnect(ctx)
}

// ListCollections returns all collection names in the given database,
// filtering out system collections.
func (c *Client) ListCollections(ctx context.Context, database string) ([]string, error) {
	db := c.inner.Database(database)
	names, err := db.ListCollectionNames(ctx, bson.M{})
	if err != nil {
		return nil, fmt.Errorf("failed to list collections on %s: %w", RedactURI(c.uri), err)
	}
	return names, nil
}

// FetchIDs returns all _id values from a collection using a projected query.
func (c *Client) FetchIDs(ctx context.Context, database, collection string) ([]interface{}, error) {
	coll := c.inner.Database(database).Collection(collection)
	cursor, err := coll.Find(ctx, bson.M{}, options.Find().SetProjection(bson.M{"_id": 1}))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch IDs from %s.%s: %w", database, collection, err)
	}
	defer cursor.Close(ctx)

	var results []bson.M
	if err := cursor.All(ctx, &results); err != nil {
		return nil, fmt.Errorf("failed to read IDs from %s.%s: %w", database, collection, err)
	}

	ids := make([]interface{}, len(results))
	for i, doc := range results {
		ids[i] = doc["_id"]
	}
	return ids, nil
}

// FetchDocuments returns full documents for the given _id values.
func (c *Client) FetchDocuments(ctx context.Context, database, collection string, ids []interface{}) (map[interface{}]bson.M, error) {
	if len(ids) == 0 {
		return make(map[interface{}]bson.M), nil
	}

	coll := c.inner.Database(database).Collection(collection)
	cursor, err := coll.Find(ctx, bson.M{"_id": bson.M{"$in": ids}})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch documents from %s.%s: %w", database, collection, err)
	}
	defer cursor.Close(ctx)

	docs := make(map[interface{}]bson.M, len(ids))
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			return nil, fmt.Errorf("failed to decode document from %s.%s: %w", database, collection, err)
		}
		docs[doc["_id"]] = doc
	}
	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("cursor error reading %s.%s: %w", database, collection, err)
	}

	return docs, nil
}

// FetchAllDocuments returns all documents in a collection keyed by _id.
func (c *Client) FetchAllDocuments(ctx context.Context, database, collection string) (map[interface{}]bson.M, error) {
	coll := c.inner.Database(database).Collection(collection)
	cursor, err := coll.Find(ctx, bson.M{})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch documents from %s.%s: %w", database, collection, err)
	}
	defer cursor.Close(ctx)

	docs := make(map[interface{}]bson.M)
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			return nil, fmt.Errorf("failed to decode document from %s.%s: %w", database, collection, err)
		}
		docs[doc["_id"]] = doc
	}
	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("cursor error reading %s.%s: %w", database, collection, err)
	}

	return docs, nil
}

// ListDatabases returns available database names.
func (c *Client) ListDatabases(ctx context.Context) ([]string, error) {
	result, err := c.inner.ListDatabaseNames(ctx, bson.M{})
	if err != nil {
		return nil, fmt.Errorf("failed to list databases: %w", err)
	}
	return result, nil
}

// InsertDocuments inserts multiple documents into a collection.
func (c *Client) InsertDocuments(ctx context.Context, database, collection string, docs []bson.M) error {
	if len(docs) == 0 {
		return nil
	}
	coll := c.inner.Database(database).Collection(collection)
	ifaces := make([]interface{}, len(docs))
	for i, d := range docs {
		ifaces[i] = d
	}
	_, err := coll.InsertMany(ctx, ifaces)
	if err != nil {
		return fmt.Errorf("failed to insert %d documents into %s.%s: %w", len(docs), database, collection, err)
	}
	return nil
}

// ReplaceDocument replaces a single document by _id.
func (c *Client) ReplaceDocument(ctx context.Context, database, collection string, id interface{}, doc bson.M) error {
	coll := c.inner.Database(database).Collection(collection)
	_, err := coll.ReplaceOne(ctx, bson.M{"_id": id}, doc)
	if err != nil {
		return fmt.Errorf("failed to replace document in %s.%s: %w", database, collection, err)
	}
	return nil
}

// UpsertDocument replaces a document if it exists, or inserts it if it doesn't.
func (c *Client) UpsertDocument(ctx context.Context, database, collection string, doc bson.M) error {
	coll := c.inner.Database(database).Collection(collection)
	id, ok := doc["_id"]
	if !ok {
		return fmt.Errorf("document has no _id field")
	}
	opts := options.Replace().SetUpsert(true)
	_, err := coll.ReplaceOne(ctx, bson.M{"_id": id}, doc, opts)
	if err != nil {
		return fmt.Errorf("failed to upsert document in %s.%s: %w", database, collection, err)
	}
	return nil
}

// DeleteDocuments deletes documents by their _id values.
func (c *Client) DeleteDocuments(ctx context.Context, database, collection string, ids []interface{}) error {
	if len(ids) == 0 {
		return nil
	}
	coll := c.inner.Database(database).Collection(collection)
	_, err := coll.DeleteMany(ctx, bson.M{"_id": bson.M{"$in": ids}})
	if err != nil {
		return fmt.Errorf("failed to delete documents from %s.%s: %w", database, collection, err)
	}
	return nil
}

// CreateCollection creates a new collection.
func (c *Client) CreateCollection(ctx context.Context, database, collection string) error {
	return c.inner.Database(database).CreateCollection(ctx, collection)
}

// DropCollection drops a collection.
func (c *Client) DropCollection(ctx context.Context, database, collection string) error {
	return c.inner.Database(database).Collection(collection).Drop(ctx)
}

// RedactURI removes password from a MongoDB connection string for safe display.
func RedactURI(uri string) string {
	parsed, err := url.Parse(uri)
	if err != nil {
		return "(invalid URI)"
	}
	if parsed.User != nil {
		if _, hasPass := parsed.User.Password(); hasPass {
			parsed.User = url.UserPassword(parsed.User.Username(), "***")
		}
	}
	return parsed.String()
}
