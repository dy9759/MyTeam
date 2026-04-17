package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

type fakeMemberLookup struct {
	role string
	err  error
}

func (f fakeMemberLookup) GetMemberRole(_ context.Context, _ uuid.UUID, _ uuid.UUID) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.role, nil
}

type fakeAgentLookup struct {
	ownerID uuid.UUID
	err     error
}

func (f fakeAgentLookup) GetAgentOwnerID(_ context.Context, _ uuid.UUID) (uuid.UUID, error) {
	if f.err != nil {
		return uuid.Nil, f.err
	}
	return f.ownerID, nil
}

func TestRequireAdminOrAbove_AllowsAdmin(t *testing.T) {
	g := Guards{Member: fakeMemberLookup{role: "admin"}}
	if err := g.RequireAdminOrAbove(context.Background(), uuid.New(), uuid.New()); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestRequireAdminOrAbove_AllowsOwner(t *testing.T) {
	g := Guards{Member: fakeMemberLookup{role: "owner"}}
	if err := g.RequireAdminOrAbove(context.Background(), uuid.New(), uuid.New()); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestRequireAdminOrAbove_RejectsMember(t *testing.T) {
	g := Guards{Member: fakeMemberLookup{role: "member"}}
	err := g.RequireAdminOrAbove(context.Background(), uuid.New(), uuid.New())
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("expected ErrForbidden, got %v", err)
	}
}

func TestRequireOwner_OnlyOwner(t *testing.T) {
	g := Guards{Member: fakeMemberLookup{role: "admin"}}
	if !errors.Is(g.RequireOwner(context.Background(), uuid.New(), uuid.New()), ErrForbidden) {
		t.Error("expected admin to be rejected by RequireOwner")
	}
	g.Member = fakeMemberLookup{role: "owner"}
	if err := g.RequireOwner(context.Background(), uuid.New(), uuid.New()); err != nil {
		t.Errorf("expected owner allowed, got %v", err)
	}
	g.Member = fakeMemberLookup{role: "member"}
	if !errors.Is(g.RequireOwner(context.Background(), uuid.New(), uuid.New()), ErrForbidden) {
		t.Error("expected member to be rejected by RequireOwner")
	}
}

func TestRequireAgentOwner_AllowsOwner(t *testing.T) {
	user := uuid.New()
	agent := uuid.New()
	g := Guards{Agent: fakeAgentLookup{ownerID: user}}
	if err := g.RequireAgentOwner(context.Background(), agent, user); err != nil {
		t.Errorf("expected owner allowed, got %v", err)
	}
}

func TestRequireAgentOwner_RejectsOther(t *testing.T) {
	owner := uuid.New()
	other := uuid.New()
	g := Guards{Agent: fakeAgentLookup{ownerID: owner}}
	err := g.RequireAgentOwner(context.Background(), uuid.New(), other)
	if !errors.Is(err, ErrForbidden) {
		t.Error("expected non-owner to be forbidden")
	}
}

func TestRequireAgentOwner_PropagatesLookupError(t *testing.T) {
	want := errors.New("lookup failed")
	g := Guards{Agent: fakeAgentLookup{err: want}}
	err := g.RequireAgentOwner(context.Background(), uuid.New(), uuid.New())
	if !errors.Is(err, want) {
		t.Errorf("expected lookup error, got %v", err)
	}
}
