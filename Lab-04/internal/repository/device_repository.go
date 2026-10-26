package repository

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"deviceregistry/internal/domain"
)

// DeviceRepository визначає контракт для збереження та отримання пристроїв
type DeviceRepository interface {
	GetAll(filter domain.DeviceFilter) []domain.Device
	GetByID(id string) (domain.Device, bool)
	Create(req domain.CreateDeviceRequest) domain.Device
	Update(id string, req domain.UpdateDeviceRequest) (domain.Device, bool)
	Delete(id string) bool
}

// MemoryDeviceRepository реалізує in-memory сховище із захистом через sync.RWMutex
type MemoryDeviceRepository struct {
	mu      sync.RWMutex
	storage map[string]domain.Device
	counter int
}

// NewMemoryDeviceRepository створює новий екземпляр сховища в пам'яті
func NewMemoryDeviceRepository() *MemoryDeviceRepository {
	return &MemoryDeviceRepository{
		storage: make(map[string]domain.Device),
	}
}

// GetAll повертає список пристроїв із можливістю фільтрації за status та type (Рівень 1 та 3)
func (r *MemoryDeviceRepository) GetAll(filter domain.DeviceFilter) []domain.Device {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]domain.Device, 0, len(r.storage))

	for _, d := range r.storage {
		// Фільтрація за статусом
		if filter.Status != "" && !strings.EqualFold(d.Status, filter.Status) {
			continue
		}
		// Фільтрація за типом
		if filter.Type != "" && !strings.EqualFold(d.Type, filter.Type) {
			continue
		}
		result = append(result, d)
	}

	return result
}

// GetByID знаходить пристрій за унікальним ідентифікатором (Рівень 1)
func (r *MemoryDeviceRepository) GetByID(id string) (domain.Device, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	device, exists := r.storage[id]
	return device, exists
}

// Create генерує унікальний ID, зберігає пристрій та повертає створену сутність (Рівень 1)
func (r *MemoryDeviceRepository) Create(req domain.CreateDeviceRequest) domain.Device {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.counter++
	id := fmt.Sprintf("dev-%03d", r.counter)

	status := req.Status
	if status == "" {
		status = "online"
	}

	device := domain.Device{
		ID:        id,
		Name:      req.Name,
		IPAddress: req.IPAddress,
		Type:      req.Type,
		Status:    status,
		UpdatedAt: time.Now().UTC(),
	}

	r.storage[id] = device
	return device
}

// Update оновлює поля існуючого пристрою (Рівень 2)
func (r *MemoryDeviceRepository) Update(id string, req domain.UpdateDeviceRequest) (domain.Device, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	device, exists := r.storage[id]
	if !exists {
		return domain.Device{}, false
	}

	if req.Name != "" {
		device.Name = req.Name
	}
	if req.IPAddress != "" {
		device.IPAddress = req.IPAddress
	}
	if req.Type != "" {
		device.Type = req.Type
	}
	if req.Status != "" {
		device.Status = req.Status
	}
	device.UpdatedAt = time.Now().UTC()

	r.storage[id] = device
	return device, true
}

// Delete видаляє пристрій зі сховища (Рівень 2)
func (r *MemoryDeviceRepository) Delete(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.storage[id]; !exists {
		return false
	}

	delete(r.storage, id)
	return true
}
