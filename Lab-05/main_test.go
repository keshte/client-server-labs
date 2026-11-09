package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestIncidentTrackerAPI(t *testing.T) {
	// Use isolated in-memory or temp sqlite
	_ = os.Remove("test_incidents.db")
	defer os.Remove("test_incidents.db")

	var err error
	DB, err = gorm.Open(sqlite.Open("test_incidents.db"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to open db: %v", err)
	}
	_ = DB.AutoMigrate(&User{}, &Incident{})

	r := setupRouter()

	// 1. Test Registration
	regBody := RegisterDTO{
		Email:    "engineer@company.internal",
		Password: "SuperSecretPassword123",
	}
	b, _ := json.Marshal(regBody)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/auth/register", bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("Expected 201 on register, got %d", w.Code)
	}

	// 2. Test Login
	loginBody := LoginDTO{
		Email:    "engineer@company.internal",
		Password: "SuperSecretPassword123",
	}
	b, _ = json.Marshal(loginBody)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/v1/auth/login", bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200 on login, got %d", w.Code)
	}

	var loginResp map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &loginResp)
	token := loginResp["token"]
	if token == "" {
		t.Fatalf("Token is empty")
	}

	// 3. Test Invalid DTO (validation test: priority "urgent" is not allowed)
	invalidDTO := map[string]string{
		"title":       "Test Incident",
		"description": "Short",
		"priority":    "urgent",
	}
	b, _ = json.Marshal(invalidDTO)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/v1/incidents", bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400 on invalid priority, got %d", w.Code)
	}

	// 4. Test Valid Incident Creation
	validDTO := CreateIncidentDTO{
		Title:       "Overheating on Gateway-01",
		Description: "Core temperature reached 85C",
		Priority:    "critical",
	}
	b, _ = json.Marshal(validDTO)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/v1/incidents", bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("Expected 201 on create incident, got %d", w.Code)
	}

	// 5. Test Unauthorized Access (delete without token)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("DELETE", "/api/v1/incidents/1", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("Expected 401 on unauthorized delete, got %d", w.Code)
	}

	// 6. Test Status Patch with token
	statusDTO := UpdateStatusDTO{Status: "in_progress"}
	b, _ = json.Marshal(statusDTO)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PATCH", "/api/v1/incidents/1/status", bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200 on status patch, got %d", w.Code)
	}

	// 7. Test Public List Incidents
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/v1/incidents", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200 on list incidents, got %d", w.Code)
	}
}
