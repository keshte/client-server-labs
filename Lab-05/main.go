package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// --- JWT Claims ---
type CustomClaims struct {
	UserID uint   `json:"userID"`
	Email  string `json:"email"`
	jwt.RegisteredClaims
}

var jwtSecret = []byte("super-secret-incident-key-2026-very-secure-min-32-bytes")

// --- Models ---
type User struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Email        string    `gorm:"uniqueIndex;not null" json:"email"`
	PasswordHash string    `gorm:"not null" json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

type Incident struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Title       string    `gorm:"size:100;not null" json:"title"`
	Description string    `gorm:"type:text;not null" json:"description"`
	Priority    string    `gorm:"size:20;not null" json:"priority"`
	Status      string    `gorm:"size:20;default:'open'" json:"status"`
	UserID      uint      `gorm:"default:1" json:"user_id"`
	CreatedAt   time.Time `json:"created_at"`
}

// --- DTOs ---
type CreateIncidentDTO struct {
	Title       string `json:"title" binding:"required,min=3,max=100"`
	Description string `json:"description" binding:"required"`
	Priority    string `json:"priority" binding:"required,oneof=low medium high critical"`
}

type UpdateStatusDTO struct {
	Status string `json:"status" binding:"required,oneof=open in_progress resolved closed"`
}

type RegisterDTO struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
}

type LoginDTO struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

// --- Database Init ---
var DB *gorm.DB

func initDB() {
	var err error
	dsn := os.Getenv("DATABASE_DSN")
	if dsn == "" {
		dsn = "host=localhost user=postgres password=postgres dbname=postgres port=5432 sslmode=disable"
	}

	// Try Postgres first if available
	DB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Printf("[GORM] PostgreSQL unavailable at %s, falling back to local SQLite engine: %v", dsn, err)
		DB, err = gorm.Open(sqlite.Open("incidents.db"), &gorm.Config{})
		if err != nil {
			log.Fatalf("[GORM] Critical: failed to initialize database: %v", err)
		}
		log.Println("[GORM] Connected to SQLite database 'incidents.db' successfully")
	} else {
		log.Println("[GORM] Connected to PostgreSQL successfully (host=localhost:5432, db=postgres)")
	}

	// Auto-migration
	if err := DB.AutoMigrate(&User{}, &Incident{}); err != nil {
		log.Fatalf("[GORM] Auto-migration error: %v", err)
	}
	log.Println("[GORM] Auto-migration completed: 'users', 'incidents' tables are up to date.")
}

// --- JWT Auth Middleware ---
func AuthMiddleware(secret []byte) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized access"})
			return
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		token, err := jwt.ParseWithClaims(tokenString, &CustomClaims{}, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, errors.New("unexpected signing method")
			}
			return secret, nil
		})

		if err != nil || !token.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized access"})
			return
		}

		claims, ok := token.Claims.(*CustomClaims)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized access"})
			return
		}

		c.Set("userID", claims.UserID)
		c.Set("userEmail", claims.Email)
		c.Next()
	}
}

// --- Handlers ---
func RegisterHandler(c *gin.Context) {
	var dto RegisterDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var existing User
	if err := DB.Where("email = ?", dto.Email).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Користувач із цією поштою вже існує"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(dto.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}

	newUser := User{
		Email:        dto.Email,
		PasswordHash: string(hash),
		CreatedAt:    time.Now(),
	}

	if err := DB.Create(&newUser).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":         newUser.ID,
		"email":      newUser.Email,
		"created_at": newUser.CreatedAt,
	})
}

func LoginHandler(c *gin.Context) {
	var dto LoginDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var user User
	if err := DB.Where("email = ?", dto.Email).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Невірні облікові дані"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(dto.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Невірні облікові дані"})
		return
	}

	claims := CustomClaims{
		UserID: user.ID,
		Email:  user.Email,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "incident-tracker-api",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(jwtSecret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"token": tokenString})
}

func ListIncidentsHandler(c *gin.Context) {
	var incidents []Incident
	if err := DB.Order("created_at desc").Find(&incidents).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, incidents)
}

func GetIncidentHandler(c *gin.Context) {
	id := c.Param("id")
	var incident Incident
	if err := DB.First(&incident, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Incident not found"})
		return
	}
	c.JSON(http.StatusOK, incident)
}

func CreateIncidentHandler(c *gin.Context) {
	var dto CreateIncidentDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	val, exists := c.Get("userID")
	var uid uint = 1
	if exists {
		uid = val.(uint)
	}

	incident := Incident{
		Title:       dto.Title,
		Description: dto.Description,
		Priority:    dto.Priority,
		Status:      "open",
		UserID:      uid,
		CreatedAt:   time.Now(),
	}

	if err := DB.Create(&incident).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, incident)
}

func UpdateStatusHandler(c *gin.Context) {
	id := c.Param("id")
	var dto UpdateStatusDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var incident Incident
	if err := DB.First(&incident, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Incident not found"})
		return
	}

	incident.Status = dto.Status
	if err := DB.Save(&incident).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, incident)
}

func DeleteIncidentHandler(c *gin.Context) {
	id := c.Param("id")
	var incident Incident
	if err := DB.First(&incident, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Incident not found"})
		return
	}

	if err := DB.Delete(&incident).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusNoContent)
}

// Swagger HTML fallback doc page
func SwaggerDocHandler(c *gin.Context) {
	html := `<!DOCTYPE html>
<html>
<head>
  <title>Incident Tracker API - Swagger Documentation</title>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.11.0/swagger-ui.css" />
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5.11.0/swagger-ui-bundle.js"></script>
  <script>
    window.onload = function() {
      SwaggerUIBundle({
        spec: {
          openapi: "3.0.0",
          info: {
            title: "Incident Tracker API (Lab-05)",
            version: "1.0.0",
            description: "RESTful сервіс обліку інцидентів та аварій на Gin + GORM + JWT"
          },
          paths: {
            "/api/v1/auth/register": {
              post: {
                summary: "Реєстрація нового інженера",
                responses: { "201": { description: "Created" }, "400": { description: "Bad Request" } }
              }
            },
            "/api/v1/auth/login": {
              post: {
                summary: "Авторизація та отримання JWT токена",
                responses: { "200": { description: "JWT токен видано" }, "401": { description: "Невірні дані" } }
              }
            },
            "/api/v1/incidents": {
              get: {
                summary: "Публічний перегляд списку всіх інцидентів",
                responses: { "200": { description: "OK" } }
              },
              post: {
                summary: "Створення нового інциденту (потрібен JWT)",
                security: [{ bearerAuth: [] }],
                responses: { "201": { description: "Created" }, "401": { description: "Unauthorized" } }
              }
            },
            "/api/v1/incidents/{id}/status": {
              patch: {
                summary: "Зміна статусу інциденту (потрібен JWT)",
                security: [{ bearerAuth: [] }],
                responses: { "200": { description: "Updated" }, "404": { description: "Not Found" } }
              }
            },
            "/api/v1/incidents/{id}": {
              delete: {
                summary: "Видалення інциденту (потрібен JWT)",
                security: [{ bearerAuth: [] }],
                responses: { "204": { description: "No Content" } }
              }
            }
          },
          components: {
            securitySchemes: {
              bearerAuth: {
                type: "http",
                scheme: "bearer",
                bearerFormat: "JWT"
              }
            }
          }
        },
        dom_id: '#swagger-ui',
      });
    }
  </script>
</body>
</html>`
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, html)
}

func setupRouter() *gin.Engine {
	r := gin.Default()

	r.GET("/swagger/*any", SwaggerDocHandler)

	v1 := r.Group("/api/v1")
	{
		// Auth routes
		auth := v1.Group("/auth")
		{
			auth.POST("/register", RegisterHandler)
			auth.POST("/login", LoginHandler)
		}

		// Incidents public routes
		v1.GET("/incidents", ListIncidentsHandler)
		v1.GET("/incidents/:id", GetIncidentHandler)

		// Protected incidents routes
		protected := v1.Group("")
		protected.Use(AuthMiddleware(jwtSecret))
		{
			protected.POST("/incidents", CreateIncidentHandler)
			protected.PATCH("/incidents/:id/status", UpdateStatusHandler)
			protected.DELETE("/incidents/:id", DeleteIncidentHandler)
		}
	}

	return r
}

func main() {
	fmt.Println("=== Сервіс обліку інцидентів (Incident Tracker API) ===")
	initDB()
	r := setupRouter()
	fmt.Println("[СЕРВЕР] Сервіс успішно запущено на http://localhost:8080")
	fmt.Println("[СЕРВЕР] Swagger UI доступний за адресою: http://localhost:8080/swagger/index.html")
	if err := r.Run(":8080"); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
