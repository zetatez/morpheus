package session

import (
	"context"

	"github.com/zetatez/morpheus/pkg/sdk"
)

type SessionFilter struct {
	Query  string
	Limit  int
	Offset int
}

type SessionStore interface {
	Save(ctx context.Context, s *Session) error
	Get(ctx context.Context, id string) (*Session, error)
	List(ctx context.Context, filter *SessionFilter) ([]*Session, error)
	Has(ctx context.Context, id string) bool
	Close() error
}

type SessionBackend interface {
	SaveSession(ctx context.Context, sessionID string, messages []sdk.Message, summary string, meta Metadata) error
	ListSessions(ctx context.Context, query string) ([]Metadata, error)
	LoadSession(ctx context.Context, sessionID string) (StoredSession, error)
	HasSession(ctx context.Context, sessionID string) bool
	Close() error
}
