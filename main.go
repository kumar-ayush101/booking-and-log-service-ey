package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// --- 1. Define Data Structures ---

type BookingRequest struct {
	VehicleID        string           `json:"vehicleId" bson:"vehicleId"`
	ConfirmationCode string           `json:"confirmationCode" bson:"confirmationCode"`
	Status           string           `json:"status" bson:"status"`
	ScheduledService ScheduledService `json:"scheduledService" bson:"scheduledService"`
}

type ScheduledService struct {
	IsScheduled       bool   `json:"isScheduled" bson:"isScheduled"`
	ServiceCenterName string `json:"serviceCenterName" bson:"serviceCenterName"`
	DateTime          string `json:"dateTime" bson:"dateTime"`
}

type LogEntry struct {
	LogID     string  `json:"logId" bson:"logId"`
	UserID    string  `json:"userId" bson:"userId"`
	VehicleID string  `json:"vehicleId" bson:"vehicleId"`
	Timestamp string  `json:"timestamp" bson:"timestamp"`
	LogType   string  `json:"logType" bson:"logType"`
	Data      LogData `json:"data" bson:"data"`
}

type LogData struct {
	ConfirmationCode  string `json:"confirmationCode" bson:"confirmationCode"`
	Status            string `json:"status" bson:"status"`
	ServiceCenterName string `json:"serviceCenterName" bson:"serviceCenterName"`
	ScheduledAt       string `json:"scheduledAt" bson:"scheduledAt"`
	IsScheduled       bool   `json:"isScheduled" bson:"isScheduled"`
	Action            string `json:"action" bson:"action"`
}

// --- 2. Database Configuration ---

var client *mongo.Client
var bookingCollection *mongo.Collection
var logsCollection *mongo.Collection

func main() {
	// --- Load Environment Variables ---
	if err := godotenv.Load(); err != nil {
		fmt.Println("No .env file found, relying on system environment variables")
	}

	connectionString := os.Getenv("MONGO_URI")
	dbName := os.Getenv("DB_NAME")
	port := os.Getenv("PORT")

	if connectionString == "" {
		log.Fatal("MONGO_URI environment variable is not set")
	}
	if dbName == "" {
		dbName = "techathon_db"
	}
	if port == "" {
		port = "8080"
	}

	// --- 3. Connect to MongoDB ---
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	clientOptions := options.Client().ApplyURI(connectionString)
	var err error
	client, err = mongo.Connect(ctx, clientOptions)
	if err != nil {
		log.Fatal("Error creating MongoDB client:", err)
	}

	err = client.Ping(ctx, nil)
	if err != nil {
		log.Fatal("Could not connect to MongoDB:", err)
	}
	fmt.Println("Connected to MongoDB (" + dbName + ") successfully!")

	db := client.Database(dbName)
	bookingCollection = db.Collection("Bookings")
	logsCollection = db.Collection("Logs")

	// --- 4. Setup Web Server ---
	r := gin.Default()

	// CORS Middleware
	config := cors.DefaultConfig()
	config.AllowAllOrigins = true
	config.AllowHeaders = []string{"Origin", "Content-Length", "Content-Type", "Authorization"}
	r.Use(cors.New(config))

	// --- ROUTES ---
    // 1. Health Check Route (NEW) - Access via browser or GET request
	r.GET("/system-status", handleSystemStatus) 
    
    // 2. Booking Route (Existing)
	r.POST("/book-service", handleBooking)

	fmt.Println("Server starting on port " + port + "...")
	if err := r.Run(":" + port); err != nil {
		log.Fatal("Failed to run server:", err)
	}
}

// --- 5. Request Handlers ---

// NEW: Simple handler to check system status
func handleSystemStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "Active",
		"message": "System is running smoothly",
		"time":    time.Now().Format(time.RFC3339),
	})
}

func handleBooking(c *gin.Context) {
	var booking BookingRequest

	if err := c.ShouldBindJSON(&booking); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON: " + err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := bookingCollection.InsertOne(ctx, booking)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save booking"})
		return
	}
	fmt.Println("Step 1: Booking saved to 'Bookings' collection.")

	currentTimestamp := time.Now().UTC().Format(time.RFC3339)
	randNum := rand.Intn(10000)
	logID := fmt.Sprintf("LOG_%s_%04d", time.Now().Format("20060102"), randNum)

	newLog := LogEntry{
		LogID:     logID,
		UserID:    "USR_GEN_" + fmt.Sprintf("%03d", rand.Intn(100)),
		VehicleID: booking.VehicleID,
		Timestamp: currentTimestamp,
		LogType:   "BOOKING",
		Data: LogData{
			ConfirmationCode:  booking.ConfirmationCode,
			Status:            booking.Status,
			ServiceCenterName: booking.ScheduledService.ServiceCenterName,
			ScheduledAt:       booking.ScheduledService.DateTime,
			IsScheduled:       booking.ScheduledService.IsScheduled,
			Action:            "CREATED",
		},
	}

	_, err = logsCollection.InsertOne(ctx, newLog)
	if err != nil {
		fmt.Println("Error saving log:", err)
	} else {
		fmt.Println("Step 2: Log entry saved to 'Logs' collection.")
	}

	c.JSON(http.StatusOK, gin.H{
		"message":        "Successfully saved",
		"bookingStatus":  "Confirmed",
		"generatedLogId": logID,
	})
}