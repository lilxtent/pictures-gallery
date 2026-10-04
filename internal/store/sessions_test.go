package store

import (
	"context"
	"testing"
	"time"
)

func TestDeleteOtherSessionsKeepsOnlyTheGivenOne(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	for _, h := range []string{"a", "b", "c"} {
		if err := s.CreateSession(ctx, h, now.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DeleteOtherSessions(ctx, "b"); err != nil {
		t.Fatal(err)
	}
	for h, want := range map[string]bool{"a": false, "b": true, "c": false} {
		got, err := s.SessionValid(ctx, h, now)
		if err != nil || got != want {
			t.Errorf("session %q valid = %v (err %v), want %v", h, got, err, want)
		}
	}
}
