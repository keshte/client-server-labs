package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"deviceregistry/internal/handler"
	customMiddleware "deviceregistry/internal/middleware"
	"deviceregistry/internal/repository"

	"github.com/go-chi/chi/v5"
)

// SetupRouter створює та налаштовує роутер Chi з усіма middleware та маршрутами
func SetupRouter(repo repository.DeviceRepository) *chi.Mux {
	r := chi.NewRouter()

	// Підключення конвеєра Middleware
	r.Use(customMiddleware.RecovererMiddleware)
	r.Use(customMiddleware.CORSMiddleware)
	r.Use(customMiddleware.LoggerMiddleware)

	// Монтування маршрутів API з версіонуванням
	deviceHandler := handler.NewDeviceHandler(repo)
	r.Route("/api/v1", func(r chi.Router) {
		deviceHandler.RegisterRoutes(r)

		// Тестовий ендпоінт для перевірки Panic Recovery Middleware
		r.Get("/panic-test", func(w http.ResponseWriter, r *http.Request) {
			panic("тестова аварійна ситуація для перевірки Recoverer")
		})
	})

	return r
}

func main() {
	port := flag.String("port", "8080", "Порт для запуску HTTP-сервера")
	flag.Parse()

	fmt.Println("=== Мережевий реєстр пристроїв (Device Registry API) ===")

	// 1. Ініціалізація сховища даних
	repo := repository.NewMemoryDeviceRepository()
	fmt.Println("[СЕРВЕР] Ініціалізація сховища даних (sync.RWMutex: active)")

	// 2. Налаштування роутера
	router := SetupRouter(repo)
	fmt.Println("[СЕРВЕР] Конвеєр Middleware підключено: Logger, Recoverer, CORS")
	fmt.Println("[СЕРВЕР] Маршрути Chi змонтовано на префіксі /api/v1/devices")

	// 3. Запуск HTTP-сервера
	addr := ":" + *port
	server := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	go func() {
		fmt.Printf("[СЕРВЕР] HTTP-сервер запущено на порту %s...\n", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("[ПОМИЛКА] Помилка роботи сервера: %v\n", err)
			os.Exit(1)
		}
	}()

	// Очікування сигналу зупинки
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)
	<-stopChan

	fmt.Println("\n[СЕРВЕР] Отримано сигнал зупинки. Сервер вимикається...")
	_ = server.Close()
	fmt.Println("[СЕРВЕР] Сервер успішно зупинено.")
}
