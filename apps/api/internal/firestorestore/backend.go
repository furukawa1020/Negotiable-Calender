package firestorestore

import (
	"context"
	"errors"
	"fmt"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
)

type Backend struct {
	Client *firestore.Client
}

func New(ctx context.Context, projectID string) (*Backend, error) {
	if projectID == "" {
		return nil, fmt.Errorf("GCP_PROJECT_ID is required")
	}
	client, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("create firestore client: %w", err)
	}
	return &Backend{Client: client}, nil
}

func (backend *Backend) Close() error { return backend.Client.Close() }

func (backend *Backend) PingContext(ctx context.Context) error {
	_, err := backend.Client.Collections(ctx).Next()
	if errors.Is(err, iterator.Done) {
		return nil
	}
	return err
}

type Auth struct{ *Backend }
type Calendar struct{ *Backend }
type Organization struct{ *Backend }
type Policy struct{ *Backend }
type Projection struct{ *Backend }
type Request struct{ *Backend }
type Notification struct{ *Backend }
type Audit struct{ *Backend }

func (backend *Backend) Auth() *Auth                 { return &Auth{backend} }
func (backend *Backend) Calendar() *Calendar         { return &Calendar{backend} }
func (backend *Backend) Organization() *Organization { return &Organization{backend} }
func (backend *Backend) Policy() *Policy             { return &Policy{backend} }
func (backend *Backend) Projection() *Projection     { return &Projection{backend} }
func (backend *Backend) Request() *Request           { return &Request{backend} }
func (backend *Backend) Notification() *Notification { return &Notification{backend} }
func (backend *Backend) Audit() *Audit               { return &Audit{backend} }
