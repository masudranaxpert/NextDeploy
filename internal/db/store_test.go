package db

import (
	"context"
	"testing"
)

func TestStore_OwnershipAndIsolation(t *testing.T) {
	ctx := context.Background()
	// Open in-memory DB (migration runs automatically)
	store, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Failed to open DB: %v", err)
	}
	defer store.Close()

	// 1. Create Users
	adminID, err := store.CreateUser(ctx, "admin_user", "hash", RoleAdmin)
	if err != nil {
		t.Fatalf("Failed to create admin user: %v", err)
	}

	user1ID, err := store.CreateUser(ctx, "user1", "hash", RoleUser)
	if err != nil {
		t.Fatalf("Failed to create user1: %v", err)
	}

	user2ID, err := store.CreateUser(ctx, "user2", "hash", RoleUser)
	if err != nil {
		t.Fatalf("Failed to create user2: %v", err)
	}

	// 2. Create Apps
	err = store.CreateApp(ctx, "app1", "App One", user1ID)
	if err != nil {
		t.Fatalf("Failed to create app1: %v", err)
	}

	err = store.CreateApp(ctx, "app2", "App Two", user2ID)
	if err != nil {
		t.Fatalf("Failed to create app2: %v", err)
	}

	err = store.CreateApp(ctx, "app3", "App One", user2ID)
	if err != nil {
		t.Fatalf("Failed to create app with same name for different user: %v", err)
	}

	err = store.CreateApp(ctx, "app4", "App One", user1ID)
	if err == nil {
		t.Fatalf("Expected error when creating duplicate app name for the same user, got nil")
	}

	err = store.DeleteApp(ctx, "app3")
	if err != nil {
		t.Fatalf("Failed to delete app3: %v", err)
	}

	// 3. Test Isolation (ListAppsForUser)
	// User1 should only see app1
	apps1, err := store.ListAppsForUser(ctx, user1ID)
	if err != nil {
		t.Fatalf("Error listing apps for user1: %v", err)
	}
	if len(apps1) != 1 || apps1[0].ID != "app1" {
		t.Errorf("user1 should see exactly app1, got %v", apps1)
	}

	// User2 should only see app2
	apps2, err := store.ListAppsForUser(ctx, user2ID)
	if err != nil {
		t.Fatalf("Error listing apps for user2: %v", err)
	}
	if len(apps2) != 1 || apps2[0].ID != "app2" {
		t.Errorf("user2 should see exactly app2, got %v", apps2)
	}

	// Admin should see both apps (using global list or admin view check)
	allApps, err := store.ListApps(ctx)
	if err != nil {
		t.Fatalf("Error listing all apps: %v", err)
	}
	if len(allApps) != 2 {
		t.Errorf("admin should see 2 apps globally, got %d", len(allApps))
	}

	// 4. Test Collaboration (AddCollaborator)
	// Invite User1 to collaborate on User2's app2
	err = store.AddCollaborator(ctx, "app2", user1ID, CollabRoleDeveloper)
	if err != nil {
		t.Fatalf("Failed to add collaborator: %v", err)
	}

	// User1 should now see both app1 (owned) and app2 (collaborated)
	apps1PostCollab, err := store.ListAppsForUser(ctx, user1ID)
	if err != nil {
		t.Fatalf("Error listing apps for user1 after collab: %v", err)
	}
	if len(apps1PostCollab) != 2 {
		t.Errorf("user1 should see 2 apps after collaboration, got %d", len(apps1PostCollab))
	}

	collabs, err := store.ListCollaborators(ctx, "app2")
	if err != nil {
		t.Fatalf("Failed to list collaborators: %v", err)
	}
	if len(collabs) != 1 || collabs[0].UserID != user1ID || collabs[0].Role != CollabRoleDeveloper {
		t.Errorf("Expected user1 as developer collaborator, got: %v", collabs)
	}

	// 5. Test RemoveCollaborator
	err = store.RemoveCollaborator(ctx, "app2", user1ID)
	if err != nil {
		t.Fatalf("Failed to remove collaborator: %v", err)
	}

	apps1PostRemoval, err := store.ListAppsForUser(ctx, user1ID)
	if err != nil {
		t.Fatalf("Error listing apps for user1 after collab removal: %v", err)
	}
	if len(apps1PostRemoval) != 1 || apps1PostRemoval[0].ID != "app1" {
		t.Errorf("user1 should only see app1 after collab removal, got %v", apps1PostRemoval)
	}

	// 6. Test TransferAppOwnership
	// Admin transfers ownership of app1 from User1 to User2
	err = store.TransferAppOwnership(ctx, "app1", user2ID)
	if err != nil {
		t.Fatalf("Failed to transfer ownership: %v", err)
	}

	// User1 should now see 0 apps
	apps1PostTransfer, err := store.ListAppsForUser(ctx, user1ID)
	if err != nil {
		t.Fatalf("Error listing apps for user1 after transfer: %v", err)
	}
	if len(apps1PostTransfer) != 0 {
		t.Errorf("user1 should see 0 apps after transferring ownership, got %v", apps1PostTransfer)
	}

	// User2 should now see both apps
	apps2PostTransfer, err := store.ListAppsForUser(ctx, user2ID)
	if err != nil {
		t.Fatalf("Error listing apps for user2 after transfer: %v", err)
	}
	if len(apps2PostTransfer) != 2 {
		t.Errorf("user2 should see both apps after transfer, got %v", apps2PostTransfer)
	}

	// Verify Owner ID of app1 in DB
	app1, err := store.GetApp(ctx, "app1")
	if err != nil {
		t.Fatalf("Failed to get app1: %v", err)
	}
	if app1.OwnerID != user2ID {
		t.Errorf("Expected app1 owner to be user2 (%d), got %d", user2ID, app1.OwnerID)
	}

	// 7. Clean up check
	err = store.DeleteUser(ctx, adminID)
	if err != nil {
		t.Fatalf("Failed to delete admin user: %v", err)
	}
}
