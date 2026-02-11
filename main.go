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
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// --- 1. CONFIGURATION ---

// (ExternalAPIBase is no longer needed for fetching centers, but kept if you use it elsewhere)
const ExternalAPIBase = "https://admin-ey-1.onrender.com"

// --- 2. DATA STRUCTURES ---

type IncomingBookingRequest struct {
	VehicleID        string `json:"vehicleId"`
	ConfirmationCode string `json:"confirmationCode"`
	Status           string `json:"status"`
	ScheduledService struct {
		IsScheduled     bool   `json:"isScheduled"`
		ServiceCenterID string `json:"serviceCenterId"`
		DateTime        string `json:"dateTime"`
	} `json:"scheduledService"`
}

// Matches 'Bookings' schema in 'techathon_db'
type DBBooking struct {
	VehicleID        string           `json:"vehicleId" bson:"vehicleId"`
	ConfirmationCode string           `json:"confirmationCode" bson:"confirmationCode"`
	Status           string           `json:"status" bson:"status"`
	ScheduledService ScheduledService `json:"scheduledService" bson:"scheduledService"`
	UserID           string           `json:"userId,omitempty" bson:"userId,omitempty"`
}

type ScheduledService struct {
	IsScheduled     bool   `json:"isScheduled" bson:"isScheduled"`
	ServiceCenterID string `json:"serviceCenterId" bson:"serviceCenterId"`
	DateTime        string `json:"dateTime" bson:"dateTime"`
}

// Matches 'Logs' schema in 'techathon_db'
type LogEntry struct {
	LogID     string  `json:"logId" bson:"logId"`
	UserID    string  `json:"userId" bson:"userId"`
	VehicleID string  `json:"vehicleId" bson:"vehicleId"`
	Timestamp string  `json:"timestamp" bson:"timestamp"`
	LogType   string  `json:"logType" bson:"logType"`
	Data      LogData `json:"data" bson:"data"`
}

type LogData struct {
	ConfirmationCode string `json:"confirmationCode" bson:"confirmationCode"`
	Status           string `json:"status" bson:"status"`
	ServiceCenterID  string `json:"serviceCenterId" bson:"serviceCenterId"`
	ScheduledAt      string `json:"scheduledAt" bson:"scheduledAt"`
	IsScheduled      bool   `json:"isScheduled" bson:"isScheduled"`
	Action           string `json:"action" bson:"action"`
}

// Matches 'service_centers' schema in 'auto_ai_db'
// We use this for internal DB querying now, not the API response structure
type ServiceCenterDBModel struct {
	ID       string        `json:"centerId" bson:"centerId"`
	Name     string        `json:"name" bson:"name"`
	Location string        `json:"location" bson:"location"`
	Capacity int           `json:"capacity" bson:"capacity"`
	Bookings []interface{} `json:"bookings" bson:"bookings"` // We just need the length
	IsActive bool          `json:"is_active" bson:"is_active"`
}

// --- 3. DATABASE SETUP ---

var client *mongo.Client

// Collections in 'techathon_db'
var bookingCollection *mongo.Collection
var logsCollection *mongo.Collection

// Collection in 'auto_ai_db' database
var serviceCenterCollection *mongo.Collection

func main() {
	if err := godotenv.Load(); err != nil {
		fmt.Println("No .env file found")
	}

	connectionString := os.Getenv("MONGO_URI")
	dbName := os.Getenv("DB_NAME") // This should be 'techathon_db'
	port := os.Getenv("PORT")

	if connectionString == "" {
		log.Fatal("MONGO_URI is not set")
	}
	if dbName == "" {
		dbName = "techathon_db"
	}
	if port == "" {
		port = "8080"
	}

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
	fmt.Println("Connected to MongoDB Cluster successfully!")

	// ... inside main(), after client.Ping ...

    fmt.Println("Connected to MongoDB Cluster successfully!")

    // --- DEBUG: LIST DATABASES ---
    // This proves if Render can actually "see" the auto_ai_db
    databases, err := client.ListDatabaseNames(ctx, bson.M{})
    if err != nil {
        fmt.Println("❌ Error listing databases:", err)
    } else {
        fmt.Println("✅ Available Databases:", databases)
    }
    // -----------------------------

    // 1. Access 'techathon_db' ...

	// --- CRITICAL: Accessing Two Different Databases ---

	// 1. Access 'techathon_db' (from .env)
	techathonDB := client.Database(dbName)
	bookingCollection = techathonDB.Collection("Bookings")
	logsCollection = techathonDB.Collection("Logs")
	fmt.Println("Linked to Database:", dbName)

	// 2. Access 'auto_ai_db' database
	adminDB := client.Database("auto_ai_db")
	serviceCenterCollection = adminDB.Collection("service_centers")
	fmt.Println("Linked to Database: auto_ai_db (for service_centers updates)")

	r := gin.Default()
	config := cors.DefaultConfig()
	config.AllowAllOrigins = true
	config.AllowHeaders = []string{"Origin", "Content-Length", "Content-Type", "Authorization"}
	r.Use(cors.New(config))

	r.GET("/system-status", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "Active"})
	})

	// --- ROUTES ---
	r.GET("/bookings", handleGetAllBookings)
	r.GET("/logs", handleGetAllLogs)
	r.POST("/book-service", handleBooking)

	fmt.Println("Server starting on port " + port + "...")
	r.Run(":" + port)
}

// --- 4. HANDLERS ---

func handleGetAllLogs(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cursor, err := logsCollection.Find(ctx, bson.M{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch logs"})
		return
	}
	defer cursor.Close(ctx)

	var logs []LogEntry
	if err = cursor.All(ctx, &logs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error decoding logs"})
		return
	}

	c.JSON(http.StatusOK, logs)
}

func handleGetAllBookings(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cursor, err := bookingCollection.Find(ctx, bson.M{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch"})
		return
	}
	defer cursor.Close(ctx)
	var bookings []DBBooking
	cursor.All(ctx, &bookings)
	c.JSON(http.StatusOK, bookings)
}

func handleBooking(c *gin.Context) {
	var req IncomingBookingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON: " + err.Error()})
		return
	}

	finalCenterID := req.ScheduledService.ServiceCenterID
	isAutoAssigned := false

	// --- STEP 1: Determine Center ID (DB Logic) ---
	if finalCenterID == "" || finalCenterID == "null" {
		fmt.Println("⚠️ Center ID missing. Querying DB for least busy center...")

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Fetch all active centers
		filter := bson.M{"is_active": true}
		cursor, err := serviceCenterCollection.Find(ctx, filter)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query service centers"})
			return
		}
		defer cursor.Close(ctx)

		var centers []ServiceCenterDBModel
		if err = cursor.All(ctx, &centers); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to decode service centers"})
			return
		}

		// Algorithm: Find Least Occupied
		var bestCenter *ServiceCenterDBModel
		minBookings := 999999

		for i := range centers {
			// --- CRITICAL FIX: Skip centers with missing IDs ---
			if centers[i].ID == "" {
				fmt.Printf("⚠️ Skipping Center '%s' (Missing centerId in DB)\n", centers[i].Name)
				continue
			}

			currentLoad := len(centers[i].Bookings)
			
			// Check if this center is less busy than the current minimum
			if currentLoad < minBookings {
				minBookings = currentLoad
				bestCenter = &centers[i]
			}
		}

		if bestCenter == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "No valid service centers available (Check DB for missing centerIds)"})
			return
		}

		finalCenterID = bestCenter.ID
		isAutoAssigned = true
		fmt.Printf("✅ Auto-assigned: %s (%s) | Current Load: %d\n", bestCenter.Name, finalCenterID, minBookings)
	}

	// --- STEP 2: Save to 'techathon_db' -> 'Bookings' ---
	bookingToSave := DBBooking{
		VehicleID:        req.VehicleID,
		ConfirmationCode: req.ConfirmationCode,
		Status:           req.Status,
		ScheduledService: ScheduledService{
			IsScheduled:     req.ScheduledService.IsScheduled,
			ServiceCenterID: finalCenterID,
			DateTime:        req.ScheduledService.DateTime,
		},
		UserID: "USR_" + req.VehicleID, 
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := bookingCollection.InsertOne(ctx, bookingToSave); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "DB Save Failed"})
		return
	}

	// --- STEP 3: Save to 'techathon_db' -> 'Logs' ---
	randNum := rand.Intn(10000)
	logID := fmt.Sprintf("LOG_%s_%04d", time.Now().Format("20060102"), randNum)

	logEntry := LogEntry{
		LogID:     logID,
		UserID:    bookingToSave.UserID,
		VehicleID: req.VehicleID,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		LogType:   "BOOKING",
		Data: LogData{
			ConfirmationCode: req.ConfirmationCode,
			Status:           req.Status,
			ServiceCenterID:  finalCenterID,
			ScheduledAt:      req.ScheduledService.DateTime,
			IsScheduled:      req.ScheduledService.IsScheduled,
			Action:           "CREATED",
		},
	}
	if isAutoAssigned {
		logEntry.Data.Action = "AUTO_ASSIGNED_CREATED"
	}
	logsCollection.InsertOne(ctx, logEntry)

	// --- STEP 4: Update 'auto_ai_db' -> 'service_centers' ---
	go func() {
		// Use a fresh context for background task
		bgCtx, bgCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer bgCancel()

		fmt.Printf("🔄 Updating 'auto_ai_db' -> Center: %s\n", finalCenterID)

		filter := bson.M{"centerId": finalCenterID}
		update := bson.M{
			"$push": bson.M{
				"bookings": bookingToSave,
			},
		}

		result, err := serviceCenterCollection.UpdateOne(bgCtx, filter, update)
		if err != nil {
			fmt.Printf("❌ DB Update Failed: %v\n", err)
		} else {
			// DEBUG: Print if we actually modified anything
			if result.ModifiedCount == 0 {
				fmt.Printf("⚠️ Warning: Update ran but modified 0 documents. Check if centerId '%s' exists in DB.\n", finalCenterID)
			} else {
				fmt.Printf("✅ DB Update Success. Modified Count: %d\n", result.ModifiedCount)
			}
		}
	}()

	// Response
	c.JSON(http.StatusOK, gin.H{
		"bookingStatus":  "Confirmed",
		"generatedLogId": logID,
		"assignedCenter": finalCenterID,
		"message":        "Successfully saved",
	})
}