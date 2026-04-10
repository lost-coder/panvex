package auth

import (
	"testing"
	"time"
)

func TestUpdateUserRevokesSessionsOnPasswordChange(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	service := NewService()
	service.SetNow(func() time.Time { return now.Add(time.Minute) })

	user, _, err := service.BootstrapUser(BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     RoleAdmin,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	session, err := service.Authenticate(LoginInput{
		Username: "admin",
		Password: "Admin1password",
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	if _, err := service.GetSession(session.ID); err != nil {
		t.Fatalf("GetSession() before update error = %v", err)
	}

	_, err = service.UpdateUser(UpdateUserInput{
		UserID:      user.ID,
		Username:    "admin",
		Role:        RoleAdmin,
		NewPassword: "NewAdmin1password",
	}, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("UpdateUser() error = %v", err)
	}

	if _, err := service.GetSession(session.ID); err == nil {
		t.Fatal("GetSession() after password change error = nil, want session revoked")
	}
}

func TestUpdateUserRevokesSessionsOnRoleDemotion(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	service := NewService()
	service.SetNow(func() time.Time { return now.Add(time.Minute) })

	user, _, err := service.BootstrapUser(BootstrapInput{
		Username: "admin1",
		Password: "Admin1password",
		Role:     RoleAdmin,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() first admin error = %v", err)
	}

	// Create a second admin so demotion of the first is allowed.
	_, _, err = service.BootstrapUser(BootstrapInput{
		Username: "admin2",
		Password: "Admin2password",
		Role:     RoleAdmin,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() second admin error = %v", err)
	}

	session, err := service.Authenticate(LoginInput{
		Username: "admin1",
		Password: "Admin1password",
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	if _, err := service.GetSession(session.ID); err != nil {
		t.Fatalf("GetSession() before demotion error = %v", err)
	}

	_, err = service.UpdateUser(UpdateUserInput{
		UserID:   user.ID,
		Username: "admin1",
		Role:     RoleOperator,
	}, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("UpdateUser() error = %v", err)
	}

	if _, err := service.GetSession(session.ID); err == nil {
		t.Fatal("GetSession() after role demotion error = nil, want session revoked")
	}
}

func TestUpdateUserKeepsSessionsOnUsernameChange(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	service := NewService()
	service.SetNow(func() time.Time { return now.Add(time.Minute) })

	user, _, err := service.BootstrapUser(BootstrapInput{
		Username: "operator",
		Password: "Correct1horse2battery",
		Role:     RoleOperator,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	session, err := service.Authenticate(LoginInput{
		Username: "operator",
		Password: "Correct1horse2battery",
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	_, err = service.UpdateUser(UpdateUserInput{
		UserID:   user.ID,
		Username: "operator-renamed",
		Role:     RoleOperator,
	}, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("UpdateUser() error = %v", err)
	}

	if _, err := service.GetSession(session.ID); err != nil {
		t.Fatalf("GetSession() after username change error = %v, want session preserved", err)
	}
}

func TestUpdateUserKeepsSessionsOnRolePromotion(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	service := NewService()
	service.SetNow(func() time.Time { return now.Add(time.Minute) })

	user, _, err := service.BootstrapUser(BootstrapInput{
		Username: "viewer",
		Password: "Viewer1password",
		Role:     RoleViewer,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	session, err := service.Authenticate(LoginInput{
		Username: "viewer",
		Password: "Viewer1password",
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	_, err = service.UpdateUser(UpdateUserInput{
		UserID:   user.ID,
		Username: "viewer",
		Role:     RoleOperator,
	}, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("UpdateUser() error = %v", err)
	}

	if _, err := service.GetSession(session.ID); err != nil {
		t.Fatalf("GetSession() after role promotion error = %v, want session preserved", err)
	}
}
