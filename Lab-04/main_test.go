package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"deviceregistry/internal/domain"
	"deviceregistry/internal/repository"
)

func newTestServer() (http.Handler, repository.DeviceRepository) {
	repo := repository.NewMemoryDeviceRepository()
	router := SetupRouter(repo)
	return router, repo
}

func TestDeviceFullCRUDLifecycle(t *testing.T) {
	handler, _ := newTestServer()

	// 1. Create (POST /api/v1/devices)
	createBody := []byte(`{
		"name": "Main Thermal Sensor",
		"ip_address": "192.168.1.50",
		"type": "sensor",
		"status": "online"
	}`)
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/devices", bytes.NewBuffer(createBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	var created domain.Device
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("failed to decode created device: %v", err)
	}

	if created.ID == "" || created.Name != "Main Thermal Sensor" {
		t.Errorf("unexpected created device: %+v", created)
	}

	// 2. Read (GET /api/v1/devices/{id})
	req, _ = http.NewRequest(http.MethodGet, "/api/v1/devices/"+created.ID, nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	// 3. Update (PUT /api/v1/devices/{id})
	updateBody := []byte(`{
		"name": "Updated Thermal Sensor",
		"status": "offline"
	}`)
	req, _ = http.NewRequest(http.MethodPut, "/api/v1/devices/"+created.ID, bytes.NewBuffer(updateBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	var updated domain.Device
	_ = json.Unmarshal(rec.Body.Bytes(), &updated)
	if updated.Name != "Updated Thermal Sensor" || updated.Status != "offline" {
		t.Errorf("update failed, got: %+v", updated)
	}

	// 4. Delete (DELETE /api/v1/devices/{id})
	req, _ = http.NewRequest(http.MethodDelete, "/api/v1/devices/"+created.ID, nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 No Content, got %d", rec.Code)
	}
	if rec.Body.Len() > 0 {
		t.Errorf("204 No Content should not have body, got: %s", rec.Body.String())
	}

	// 5. Read again -> 404 Not Found
	req, _ = http.NewRequest(http.MethodGet, "/api/v1/devices/"+created.ID, nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found after deletion, got %d", rec.Code)
	}

	var errResp domain.ErrorResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
	if errResp.Error != "Resource Not Found" || errResp.Status != 404 {
		t.Errorf("unexpected error format: %+v", errResp)
	}
}

func TestQueryFiltering(t *testing.T) {
	handler, repo := newTestServer()

	// Наповнення репозиторію
	repo.Create(domain.CreateDeviceRequest{Name: "S1", IPAddress: "10.0.0.1", Type: "sensor", Status: "online"})
	repo.Create(domain.CreateDeviceRequest{Name: "S2", IPAddress: "10.0.0.2", Type: "sensor", Status: "offline"})
	repo.Create(domain.CreateDeviceRequest{Name: "G1", IPAddress: "10.0.0.3", Type: "gateway", Status: "online"})

	testCases := []struct {
		url           string
		expectedCount int
	}{
		{"/api/v1/devices", 3},
		{"/api/v1/devices?status=online", 2},
		{"/api/v1/devices?type=sensor", 2},
		{"/api/v1/devices?status=online&type=sensor", 1},
		{"/api/v1/devices?status=unknown", 0},
	}

	for _, tc := range testCases {
		req, _ := http.NewRequest(http.MethodGet, tc.url, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 for %s, got %d", tc.url, rec.Code)
		}

		var devices []domain.Device
		_ = json.Unmarshal(rec.Body.Bytes(), &devices)
		if len(devices) != tc.expectedCount {
			t.Errorf("for %s expected %d devices, got %d", tc.url, tc.expectedCount, len(devices))
		}
	}
}

func TestValidationErrors(t *testing.T) {
	handler, _ := newTestServer()

	// Відсутні обов'язкові поля
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/devices", bytes.NewBuffer([]byte(`{"name": ""}`)))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
	}

	// Невалідний JSON
	req, _ = http.NewRequest(http.MethodPost, "/api/v1/devices", bytes.NewBuffer([]byte(`invalid-json`)))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
	}
}

func TestCORSHeaders(t *testing.T) {
	handler, _ := newTestServer()

	req, _ := http.NewRequest(http.MethodOptions, "/api/v1/devices", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204 for OPTIONS, got %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("missing CORS origin header")
	}
	if rec.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Errorf("missing CORS methods header")
	}
}

func TestPanicRecoveryMiddleware(t *testing.T) {
	handler, _ := newTestServer()

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/panic-test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 Internal Server Error after panic, got %d", rec.Code)
	}

	var errResp domain.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to decode panic response: %v", err)
	}

	if errResp.Error != "Internal Server Error" || errResp.Status != 500 {
		t.Errorf("unexpected panic response: %+v", errResp)
	}
}

func TestConcurrentRepositoryAccess(t *testing.T) {
	repo := repository.NewMemoryDeviceRepository()

	var wg sync.WaitGroup
	// 50 горутин пишуть, 50 читають одночасно
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(idx int) {
			defer wg.Done()
			repo.Create(domain.CreateDeviceRequest{
				Name:      fmt.Sprintf("Dev-%d", idx),
				IPAddress: fmt.Sprintf("192.168.1.%d", idx),
				Type:      "sensor",
			})
		}(i)

		go func() {
			defer wg.Done()
			_ = repo.GetAll(domain.DeviceFilter{})
		}()
	}

	wg.Wait()
	devices := repo.GetAll(domain.DeviceFilter{})
	if len(devices) != 50 {
		t.Errorf("expected 50 devices, got %d", len(devices))
	}
}
