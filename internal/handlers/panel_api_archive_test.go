package handlers

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"panel/internal/db"
	"panel/internal/workspace"

	"github.com/gofiber/fiber/v2"
)

func createTestTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0640,
			Size: int64(len(content)),
		}
		if strings.HasSuffix(name, "/") {
			hdr.Typeflag = tar.TypeDir
			hdr.Mode = 0750
			hdr.Size = 0
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader failed: %v", err)
		}
		if len(content) > 0 {
			if _, err := tw.Write([]byte(content)); err != nil {
				t.Fatalf("Write failed: %v", err)
			}
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("tw.Close failed: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gw.Close failed: %v", err)
	}
	return buf.Bytes()
}

func TestUploadWorkspaceArchive(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "archive_test_*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := db.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open failed: %v", err)
	}
	defer store.Close()

	wsStore := workspace.NewStore(tmpDir)

	p := &Panel{
		DB:             store,
		Store:          wsStore,
		WorkspacesRoot: tmpDir,
	}

	ctx := context.Background()
	adminID, err := store.CreateUser(ctx, "adminuser", "hash", db.RoleAdmin)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	rawToken, _, err := store.CreateAPIToken(ctx, adminID, "Archive Test Token", nil, false, false, false)
	if err != nil {
		t.Fatalf("CreateAPIToken failed: %v", err)
	}

	appID := "archive-app"
	if err := store.CreateApp(ctx, appID, "Archive App", adminID); err != nil {
		t.Fatalf("CreateApp failed: %v", err)
	}
	_ = os.MkdirAll(p.Store.Path(appID), 0750)

	app := fiber.New()
	app.Post("/api/v1/apps/:id/workspace/archive", p.APIAuthMiddleware, p.UploadWorkspaceArchive)

	// 1. Unauthorized request without token
	unauthReq := httptest.NewRequest("POST", "/api/v1/apps/"+appID+"/workspace/archive", nil)
	unauthResp, err := app.Test(unauthReq)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if unauthResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized, got %d", unauthResp.StatusCode)
	}

	// 2. Successful multipart upload with valid tar.gz
	tarBytes := createTestTarGz(t, map[string]string{
		"src/":        "",
		"src/main.py": "print('hello from archive')",
		"config.json": `{"version": 1}`,
	})

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("archive", "workspace.tar.gz")
	if err != nil {
		t.Fatalf("CreateFormFile failed: %v", err)
	}
	_, _ = io.Copy(part, bytes.NewReader(tarBytes))
	_ = writer.Close()

	req := httptest.NewRequest("POST", "/api/v1/apps/"+appID+"/workspace/archive", body)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 OK, got %d: %s", resp.StatusCode, string(respBody))
	}

	var resMap map[string]interface{}
	respBody, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(respBody, &resMap); err != nil {
		t.Fatalf("failed parsing response JSON: %v", err)
	}
	if resMap["ok"] != true {
		t.Errorf("expected ok: true, got %v", resMap["ok"])
	}

	// Verify extracted files exist on disk
	mainPy := filepath.Join(p.Store.Path(appID), "src", "main.py")
	b, err := os.ReadFile(mainPy)
	if err != nil || string(b) != "print('hello from archive')" {
		t.Errorf("extracted file content mismatch: %v (content: %q)", err, string(b))
	}

	// 3. Security test: path traversal in archive must be rejected
	traversalBytes := createTestTarGz(t, map[string]string{
		"../escape.txt": "bad content",
	})
	travBody := &bytes.Buffer{}
	travWriter := multipart.NewWriter(travBody)
	travPart, _ := travWriter.CreateFormFile("archive", "bad.tar.gz")
	_, _ = io.Copy(travPart, bytes.NewReader(traversalBytes))
	_ = travWriter.Close()

	travReq := httptest.NewRequest("POST", "/api/v1/apps/"+appID+"/workspace/archive", travBody)
	travReq.Header.Set("Authorization", "Bearer "+rawToken)
	travReq.Header.Set("Content-Type", travWriter.FormDataContentType())

	travResp, err := app.Test(travReq)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if travResp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for path traversal attempt, got %d", travResp.StatusCode)
	}
}

func TestAPIAppsList(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "apps_list_test_*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := db.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open failed: %v", err)
	}
	defer store.Close()

	wsStore := workspace.NewStore(tmpDir)
	p := &Panel{
		DB:             store,
		Store:          wsStore,
		WorkspacesRoot: tmpDir,
	}

	ctx := context.Background()
	adminID, err := store.CreateUser(ctx, "adminuser", "hash", db.RoleAdmin)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	rawToken, _, err := store.CreateAPIToken(ctx, adminID, "cli-test-token", nil, false, false, false)
	if err != nil {
		t.Fatalf("CreateAPIToken failed: %v", err)
	}

	appID := "my-cli-app"
	if err := store.CreateApp(ctx, appID, "My CLI App", adminID); err != nil {
		t.Fatalf("CreateApp failed: %v", err)
	}

	app := fiber.New()
	app.Get("/api/v1/apps", p.APIAuthMiddleware, p.APIAppsList)

	// 1. Unauthorized without token
	reqUnauth := httptest.NewRequest("GET", "/api/v1/apps", nil)
	respUnauth, err := app.Test(reqUnauth)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if respUnauth.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized, got %d", respUnauth.StatusCode)
	}

	// 2. Authorized with valid token
	reqAuth := httptest.NewRequest("GET", "/api/v1/apps", nil)
	reqAuth.Header.Set("Authorization", "Bearer "+rawToken)
	respAuth, err := app.Test(reqAuth)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if respAuth.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", respAuth.StatusCode)
	}

	var apps []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	bodyBytes, _ := io.ReadAll(respAuth.Body)
	if err := json.Unmarshal(bodyBytes, &apps); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}
	if len(apps) != 1 || apps[0].ID != appID {
		t.Errorf("expected 1 app with ID %q, got %+v", appID, apps)
	}
}
