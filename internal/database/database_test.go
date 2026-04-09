package database

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestUTCImpersonationSessionsValidateInPositiveOffsetTimezone(t *testing.T) {
	originalLocal := time.Local
	time.Local = time.FixedZone("UTC+0500", 5*60*60)
	t.Cleanup(func() {
		time.Local = originalLocal
	})

	db, err := Open(filepath.Join(t.TempDir(), "fastcp.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	ctx := context.Background()
	if _, err := db.CreateUser(ctx, "admin", true); err != nil {
		t.Fatalf("create admin user: %v", err)
	}
	if _, err := db.CreateUser(ctx, "target", false); err != nil {
		t.Fatalf("create target user: %v", err)
	}

	expiresAt := time.Now().UTC().Add(2 * time.Hour)
	if err := db.CreateSession(ctx, "imp-token", "target", expiresAt); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := db.CreateImpersonationSession(ctx, &ImpersonationSession{
		Token:          "imp-token",
		AdminUsername:  "admin",
		TargetUsername: "target",
		Reason:         "support",
		ExpiresAt:      expiresAt,
	}); err != nil {
		t.Fatalf("create impersonation session: %v", err)
	}

	if err := db.CleanExpiredSessions(ctx); err != nil {
		t.Fatalf("clean expired sessions: %v", err)
	}
	if err := db.CleanExpiredImpersonationSessions(ctx); err != nil {
		t.Fatalf("clean expired impersonation sessions: %v", err)
	}

	session, err := db.GetSession(ctx, "imp-token")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if session.Username != "target" {
		t.Fatalf("unexpected session username: got %q", session.Username)
	}

	impSession, err := db.GetImpersonationSession(ctx, "imp-token")
	if err != nil {
		t.Fatalf("get impersonation session: %v", err)
	}
	if impSession.AdminUsername != "admin" || impSession.TargetUsername != "target" {
		t.Fatalf("unexpected impersonation session: %+v", impSession)
	}
}
