package firestorestore

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	coordinationrequest "github.com/negotiable-calendar/negotiable-calendar/apps/api/internal/request"
)

func (store *Request) Create(ctx context.Context, value coordinationrequest.CoordinationRequest) error {
	if err := value.Validate(); err != nil {
		return err
	}
	_, err := store.Client.Collection("coordinationRequests").Doc(value.ID).Create(ctx, value)
	if err != nil {
		return fmt.Errorf("create coordination request: %w", err)
	}
	return nil
}

func (store *Request) ListForTarget(ctx context.Context, userID string) ([]coordinationrequest.CoordinationRequest, error) {
	return store.list(ctx, "TargetUserID", userID)
}

func (store *Request) ListForRequester(ctx context.Context, userID string) ([]coordinationrequest.CoordinationRequest, error) {
	return store.list(ctx, "RequesterUserID", userID)
}

func (store *Request) ListForUser(ctx context.Context, userID string) ([]coordinationrequest.CoordinationRequest, error) {
	targeted, err := store.ListForTarget(ctx, userID)
	if err != nil {
		return nil, err
	}
	requested, err := store.ListForRequester(ctx, userID)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	values := make([]coordinationrequest.CoordinationRequest, 0, len(targeted)+len(requested))
	for _, value := range append(targeted, requested...) {
		if !seen[value.ID] {
			seen[value.ID] = true
			values = append(values, value)
		}
	}
	sortRequests(values)
	return values, nil
}

func (store *Request) list(ctx context.Context, field, userID string) ([]coordinationrequest.CoordinationRequest, error) {
	iter := store.Client.Collection("coordinationRequests").Where(field, "==", userID).Documents(ctx)
	defer iter.Stop()
	values := []coordinationrequest.CoordinationRequest{}
	for {
		doc, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("list coordination requests: %w", err)
		}
		var value coordinationrequest.CoordinationRequest
		if err := doc.DataTo(&value); err != nil {
			return nil, fmt.Errorf("decode coordination request: %w", err)
		}
		if value.Options == nil {
			value.Options = []coordinationrequest.Option{}
		}
		values = append(values, value)
	}
	sortRequests(values)
	return values, nil
}

func sortRequests(values []coordinationrequest.CoordinationRequest) {
	sort.Slice(values, func(i, j int) bool {
		if values[i].CreatedAt.Equal(values[j].CreatedAt) {
			return values[i].ID > values[j].ID
		}
		return values[i].CreatedAt.After(values[j].CreatedAt)
	})
}

func (store *Request) GetForUser(ctx context.Context, requestID, userID string) (coordinationrequest.CoordinationRequest, error) {
	var value coordinationrequest.CoordinationRequest
	doc, err := store.Client.Collection("coordinationRequests").Doc(requestID).Get(ctx)
	if firestoreNotFound(err) {
		return value, coordinationrequest.ErrNotFound
	}
	if err != nil {
		return value, fmt.Errorf("get coordination request: %w", err)
	}
	if err := doc.DataTo(&value); err != nil {
		return value, fmt.Errorf("decode coordination request: %w", err)
	}
	if value.RequesterUserID != userID && value.TargetUserID != userID {
		return coordinationrequest.CoordinationRequest{}, coordinationrequest.ErrNotFound
	}
	if value.Options == nil {
		value.Options = []coordinationrequest.Option{}
	}
	return value, nil
}

func (store *Request) Cancel(ctx context.Context, requestID, userID string) error {
	return store.mutate(ctx, requestID, func(value *coordinationrequest.CoordinationRequest, tx *firestore.Transaction) error {
		if value.RequesterUserID != userID || !oneOf(value.Status, coordinationrequest.Pending, coordinationrequest.Suggested, coordinationrequest.Delegated) {
			return coordinationrequest.ErrNotFound
		}
		value.Status, value.AcceptedOptionID, value.UpdatedAt = coordinationrequest.Cancelled, "", time.Now().UTC()
		return nil
	})
}

func (store *Request) Respond(ctx context.Context, requestID, userID string, status coordinationrequest.Status, optionID string) error {
	if status != coordinationrequest.Accepted && status != coordinationrequest.Declined {
		return fmt.Errorf("unsupported response status")
	}
	return store.mutate(ctx, requestID, func(value *coordinationrequest.CoordinationRequest, tx *firestore.Transaction) error {
		if value.TargetUserID != userID || value.Status != coordinationrequest.Suggested {
			return coordinationrequest.ErrNotFound
		}
		if status == coordinationrequest.Accepted {
			found := false
			for _, option := range value.Options {
				if option.ID == optionID {
					found = true
					break
				}
			}
			if !found {
				return coordinationrequest.ErrNotFound
			}
			value.AcceptedOptionID = optionID
		} else {
			value.AcceptedOptionID = ""
		}
		value.Status, value.UpdatedAt = status, time.Now().UTC()
		return nil
	})
}

func (store *Request) RespondAsync(ctx context.Context, requestID, userID, message string) error {
	if err := coordinationrequest.ValidateAsyncMessage(message); err != nil {
		return err
	}
	message = strings.TrimSpace(message)
	return store.mutate(ctx, requestID, func(value *coordinationrequest.CoordinationRequest, tx *firestore.Transaction) error {
		if value.TargetUserID != userID || value.Status != coordinationrequest.Suggested {
			return coordinationrequest.ErrNotFound
		}
		value.Status, value.AcceptedOptionID, value.AsyncMessage, value.UpdatedAt = coordinationrequest.Async, "", message, time.Now().UTC()
		return nil
	})
}

func (store *Request) Suggest(ctx context.Context, requestID, userID string, option coordinationrequest.Option) error {
	if option.RequestID != requestID || option.Type != coordinationrequest.OptionMeeting {
		return fmt.Errorf("invalid suggested option")
	}
	if err := option.Validate(); err != nil {
		return err
	}
	return store.mutate(ctx, requestID, func(value *coordinationrequest.CoordinationRequest, tx *firestore.Transaction) error {
		if value.TargetUserID != userID || value.Status != coordinationrequest.Suggested {
			return coordinationrequest.ErrNotFound
		}
		value.Options = append(value.Options, option)
		value.UpdatedAt = time.Now().UTC()
		return nil
	})
}

func (store *Request) Delegate(ctx context.Context, requestID, userID string, option coordinationrequest.Option) error {
	if option.RequestID != requestID || option.Type != coordinationrequest.OptionDelegate {
		return fmt.Errorf("invalid delegate option")
	}
	if err := option.Validate(); err != nil {
		return err
	}
	return store.mutate(ctx, requestID, func(value *coordinationrequest.CoordinationRequest, tx *firestore.Transaction) error {
		if value.TargetUserID != userID || value.Status != coordinationrequest.Suggested || option.DelegateUserID == userID {
			return coordinationrequest.ErrNotFound
		}
		if _, err := tx.Get(store.Client.Collection("organizations").Doc(value.OrganizationID).Collection("members").Doc(option.DelegateUserID)); err != nil {
			return coordinationrequest.ErrNotFound
		}
		value.Status, value.DelegatedUserID, value.UpdatedAt = coordinationrequest.Delegated, option.DelegateUserID, time.Now().UTC()
		value.Options = append(value.Options, option)
		return nil
	})
}

func (store *Request) mutate(ctx context.Context, id string, update func(*coordinationrequest.CoordinationRequest, *firestore.Transaction) error) error {
	ref := store.Client.Collection("coordinationRequests").Doc(id)
	err := store.Client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(ref)
		if err != nil {
			return err
		}
		var value coordinationrequest.CoordinationRequest
		if err := doc.DataTo(&value); err != nil {
			return err
		}
		if err := update(&value, tx); err != nil {
			return err
		}
		return tx.Set(ref, value)
	})
	if firestoreNotFound(err) {
		return coordinationrequest.ErrNotFound
	}
	return err
}

func oneOf(value coordinationrequest.Status, options ...coordinationrequest.Status) bool {
	for _, option := range options {
		if value == option {
			return true
		}
	}
	return false
}
