package domain

import "time"

// Device описує сутність фізичного або віртуального вузла мережі (Рівень 1)
type Device struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	IPAddress string    `json:"ip_address"`
	Type      string    `json:"type"`       // sensor, gateway, controller
	Status    string    `json:"status"`     // online, offline
	UpdatedAt time.Time `json:"updated_at"`
}

// CreateDeviceRequest описує вхідні дані для створення пристрою
type CreateDeviceRequest struct {
	Name      string `json:"name"`
	IPAddress string `json:"ip_address"`
	Type      string `json:"type"`
	Status    string `json:"status"`
}

// UpdateDeviceRequest описує вхідні дані для оновлення пристрою
type UpdateDeviceRequest struct {
	Name      string `json:"name"`
	IPAddress string `json:"ip_address"`
	Type      string `json:"type"`
	Status    string `json:"status"`
}

// DeviceFilter містить параметри для фільтрації списку пристроїв (Рівень 3)
type DeviceFilter struct {
	Status string
	Type   string
}

// ErrorResponse описує стандартизований формат повідомлень про помилки (RFC 7807, Рівень 3)
type ErrorResponse struct {
	Error     string    `json:"error"`
	Message   string    `json:"message"`
	Status    int       `json:"status"`
	Timestamp time.Time `json:"timestamp"`
}
