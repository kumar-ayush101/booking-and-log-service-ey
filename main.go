package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// --- 1. Define Data Structures ---

// BookingRequest represents the initial JSON from the user
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

// LogEntry represents the new JSON schema for the Logs collection
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

// SECURITY WARNING: In production, use Environment Variables for this connection string!
const connectionString = "mongodb+srv://jmdayushkumar_db_user:6oe935cfRww7fQZP@cluster0.iii0dcr.mongodb.net/?appName=Cluster0"
const dbName = "techathon_db" // Using the database name from your image

func main() {
	// --- 3. Connect to MongoDB ---
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	clientOptions := options.Client().ApplyURI(connectionString)
	var err error
	client, err = mongo.Connect(ctx, clientOptions)
	if err != nil {
		log.Fatal("Error creating MongoDB client:", err)
	}

	// Verify connection
	err = client.Ping(ctx, nil)
	if err != nil {
		log.Fatal("Could not connect to MongoDB:", err)
	}
	fmt.Println("Connected to MongoDB (techathon_db) successfully!")

	// Initialize Collections
	// These will be created automatically in techathon_db if they don't exist
	db := client.Database(dbName)
	bookingCollection = db.Collection("Bookings")
	logsCollection = db.Collection("Logs")

	// --- 4. Setup Web Server ---
	r := gin.Default()

	r.POST("/book-service", handleBooking)

	fmt.Println("Server starting on port 8081...")
	if err := r.Run(":8081"); err != nil {
		log.Fatal("Failed to run server:", err)
	}
}

// --- 5. Request Handler ---

func handleBooking(c *gin.Context) {
	var booking BookingRequest

	// 1. Bind JSON from request
	if err := c.ShouldBindJSON(&booking); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON: " + err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 2. Store in 'Bookings' Collection
	_, err := bookingCollection.InsertOne(ctx, booking)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save booking"})
		return
	}
	fmt.Println("Step 1: Booking saved to 'Bookings' collection.")

	// 3. Generate Data for 'Logs' Schema
	// We generate IDs and Timestamps here as requested
	currentTimestamp := time.Now().UTC().Format(time.RFC3339)
	
	// Create a random Log ID (e.g., LOG_20250203_999)
	randNum := rand.Intn(10000)
	logID := fmt.Sprintf("LOG_%s_%04d", time.Now().Format("20060102"), randNum)

	newLog := LogEntry{
		LogID:     logID,
		UserID:    "USR_GEN_" + fmt.Sprintf("%03d", rand.Intn(100)), // Generated since not provided
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

	// 4. Store in 'Logs' Collection
	_, err = logsCollection.InsertOne(ctx, newLog)
	if err != nil {
		// Note: We already saved the booking, but logging failed. 
		// We still report success for the booking, but log the error internally.
		fmt.Println("Error saving log:", err)
	} else {
		fmt.Println("Step 2: Log entry saved to 'Logs' collection.")
	}

	// 5. Send Success Message to User
	c.JSON(http.StatusOK, gin.H{
		"message":          "Successfully saved",
		"bookingStatus":    "Confirmed",
		"generatedLogId":   logID, // Optional: returning the log ID for reference
	})
}