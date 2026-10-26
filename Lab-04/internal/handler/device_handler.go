package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"deviceregistry/internal/domain"
	"deviceregistry/internal/repository"

	"github.com/go-chi/chi/v5"
)

// DeviceHandler інкапсулює HTTP-обробники для сутності Device
type DeviceHandler struct {
	repo repository.DeviceRepository
}

// NewDeviceHandler створює новий екземпляр обробника
func NewDeviceHandler(repo repository.DeviceRepository) *DeviceHandler {
	return &DeviceHandler{repo: repo}
}

// RegisterRoutes монтує маршрути сутності Device
func (h *DeviceHandler) RegisterRoutes(r chi.Router) {
	r.Route("/devices", func(r chi.Router) {
		r.Get("/", h.ListDevices)
		r.Post("/", h.CreateDevice)

		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", h.GetDevice)
			r.Put("/", h.UpdateDevice)
			r.Delete("/", h.DeleteDevice)
		})
	})
}

// writeJSON допоміжна функція для запису валідного JSON-відповіді
func writeJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(data)
}

// writeError стандартизований формат повернення помилок (RFC 7807, Рівень 3)
func writeError(w http.ResponseWriter, statusCode int, errTitle, message string) {
	resp := domain.ErrorResponse{
		Error:     errTitle,
		Message:   message,
		Status:    statusCode,
		Timestamp: time.Now().UTC(),
	}
	writeJSON(w, statusCode, resp)
}

// ListDevices повертає список пристроїв із підтримкою query-фільтрації (Рівень 1 та 3)
func (h *DeviceHandler) ListDevices(w http.ResponseWriter, r *http.Request) {
	filter := domain.DeviceFilter{
		Status: strings.TrimSpace(r.URL.Query().Get("status")),
		Type:   strings.TrimSpace(r.URL.Query().Get("type")),
	}

	devices := h.repo.GetAll(filter)
	writeJSON(w, http.StatusOK, devices)
}

// GetDevice повертає пристрій за ідентифікатором (Рівень 1)
func (h *DeviceHandler) GetDevice(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	device, exists := h.repo.GetByID(id)
	if !exists {
		writeError(w, http.StatusNotFound, "Resource Not Found", fmt.Sprintf("Device with ID '%s' does not exist in registry", id))
		return
	}

	writeJSON(w, http.StatusOK, device)
}

// CreateDevice валідує дані та створює новий пристрій (Рівень 1)
func (h *DeviceHandler) CreateDevice(w http.ResponseWriter, r *http.Request) {
	var req domain.CreateDeviceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "Некоректний формат JSON тіла запиту")
		return
	}

	// Валідація обов'язкових полів
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.IPAddress) == "" || strings.TrimSpace(req.Type) == "" {
		writeError(w, http.StatusBadRequest, "Bad Request", "Обов'язкові поля: name, ip_address, type")
		return
	}

	device := h.repo.Create(req)
	writeJSON(w, http.StatusCreated, device)
}

// UpdateDevice оновлює існуючий пристрій (Рівень 2)
func (h *DeviceHandler) UpdateDevice(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req domain.UpdateDeviceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "Некоректний формат JSON тіла запиту")
		return
	}

	updatedDevice, exists := h.repo.Update(id, req)
	if !exists {
		writeError(w, http.StatusNotFound, "Resource Not Found", fmt.Sprintf("Device with ID '%s' does not exist in registry", id))
		return
	}

	writeJSON(w, http.StatusOK, updatedDevice)
}

// DeleteDevice видаляє пристрій зі сховища (Рівень 2)
func (h *DeviceHandler) DeleteDevice(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if deleted := h.repo.Delete(id); !deleted {
		writeError(w, http.StatusNotFound, "Resource Not Found", fmt.Sprintf("Device with ID '%s' does not exist in registry", id))
		return
	}

	// 204 No Content не повинен містити тіла відповіді
	w.WriteHeader(http.StatusNoContent)
}
