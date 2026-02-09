package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// --- 1. Define Data Structures ---

// IncomingBookingRequest matches the JSON structure sent by the user
type IncomingBookingRequest struct {
	LogID     string              `json:"logId"`
	UserID    string              `json:"userId"`
	VehicleID string              `json:"vehicleId"`
	Timestamp string              `json:"timestamp"`
	LogType   string              `json:"logType"`
	Data      IncomingBookingData `json:"data"`
}

type IncomingBookingData struct {
	ConfirmationCode  string `json:"confirmationCode"`
	Status            string `json:"status"`
	ServiceCenterName string `json:"serviceCenterName"`
	ScheduledAt       string `json:"scheduledAt"`
	IsScheduled       bool   `json:"isScheduled"`
	Action            string `json:"action"`
}

// DBBooking represents how we store the booking in our local MongoDB
type DBBooking struct {
	VehicleID        string           `json:"vehicleId" bson:"vehicleId"`
	ConfirmationCode string           `json:"confirmationCode" bson:"confirmationCode"`
	Status           string           `json:"status" bson:"status"`
	ScheduledService ScheduledService `json:"scheduledService" bson:"scheduledService"`
	UserID           string           `json:"userId,omitempty" bson:"userId,omitempty"`
}

type ScheduledService struct {
	IsScheduled       bool   `json:"isScheduled" bson:"isScheduled"`
	ServiceCenterName string `json:"serviceCenterName" bson:"serviceCenterName"`
	ServiceCenterID   string `json:"serviceCenterId" bson:"serviceCenterId"`
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

// ServiceCenter represents the structure returned by the external API
type ServiceCenter struct {
	ID              interface{}      `json:"_id"` // Handle ObjectId or string
	CenterID        string           `json:"centerId"`
	Name            string           `json:"name"`
	Location        string           `json:"location"`
	Capacity        int              `json:"capacity"`
	Specializations []string         `json:"specializations"`
	Bookings        []ServiceBooking `json:"bookings"`
	IsActive        bool             `json:"is_active"`
}

// ServiceBooking represents a booking inside the ServiceCenter object
type ServiceBooking struct {
	VehicleID        string `json:"vehicleId"`
	ConfirmationCode string `json:"confirmationCode"`
	Status           string `json:"status"`
	ScheduledService struct {
		IsScheduled       bool   `json:"isScheduled"`
		ServiceCenterName string `json:"serviceCenterName"`
		DateTime          string `json:"dateTime"`
	} `json:"scheduledService"`
}

// --- 2. Database Configuration ---

var client *mongo.Client
var bookingCollection *mongo.Collection
var logsCollection *mongo.Collection

// Base URL for the external API
const BaseURL = "https://admin-ey-1.onrender.com"

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

	// 1. Health Check
	r.GET("/system-status", handleSystemStatus)

	// 2. Get All Bookings (NEW)
	r.GET("/bookings", handleGetAllBookings)

	// 3. Create Booking with Logic (UPDATED)
	r.POST("/book-service", handleBooking)

	fmt.Println("Server starting on port " + port + "...")
	if err := r.Run(":" + port); err != nil {
		log.Fatal("Failed to run server:", err)
	}
}

// --- 5. Request Handlers ---

func handleSystemStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "Active",
		"message": "System is running smoothly",
		"time":    time.Now().Format(time.RFC3339),
	})
}

// NEW: Get all bookings
func handleGetAllBookings(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cursor, err := bookingCollection.Find(ctx, bson.M{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch bookings"})
		return
	}
	defer cursor.Close(ctx)

	var bookings []DBBooking
	if err = cursor.All(ctx, &bookings); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse bookings"})
		return
	}

	c.JSON(http.StatusOK, bookings)
}

// UPDATED: Handle Booking with Smart Scheduling Logic
func handleBooking(c *gin.Context) {
	var req IncomingBookingRequest

	// 1. Parse Incoming JSON
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON: " + err.Error()})
		return
	}

	// 2. Extract Company Name (Logic: Trim part before underscore)
	// Example: PQR_999 -> PQR
	parts := strings.Split(req.VehicleID, "_")
	if len(parts) < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Vehicle ID format"})
		return
	}
	companyName := parts[0]
	fmt.Printf("Detected Company: %s from VehicleID: %s\n", companyName, req.VehicleID)

	// 3. Fetch Service Centers from External API using Company Name
	serviceCenters, err := fetchServiceCentersByName(companyName)
	if err != nil {
		fmt.Println("Error fetching service centers:", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "Could not fetch service centers for company: " + companyName})
		return
	}

	if len(serviceCenters) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "No service centers found for company: " + companyName})
		return
	}

	// 4. Find the Best Service Center (Maximum Free Capacity)
	var selectedCenter *ServiceCenter
	maxFreeSlots := -1

	for _, center := range serviceCenters {
		if center.IsActive {
			currentBookings := len(center.Bookings)
			freeSlots := center.Capacity - currentBookings

			fmt.Printf("Center: %s, Capacity: %d, Bookings: %d, Free: %d\n", center.Name, center.Capacity, currentBookings, freeSlots)

			if freeSlots > maxFreeSlots {
				maxFreeSlots = freeSlots
				// Store a copy of the center so we don't lose it
				temp := center
				selectedCenter = &temp
			}
		}
	}

	if selectedCenter == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "No active service centers found with availability"})
		return
	}

	fmt.Printf(">>> Selected Service Center: %s (ID: %s) with %d free slots\n", selectedCenter.Name, selectedCenter.CenterID, maxFreeSlots)

	// 5. Prepare Booking Data
	scheduledTime := req.Data.ScheduledAt
	if scheduledTime == "" {
		scheduledTime = time.Now().UTC().Format(time.RFC3339)
	}

	newBooking := DBBooking{
		VehicleID:        req.VehicleID,
		ConfirmationCode: req.Data.ConfirmationCode,
		Status:           req.Data.Status,
		UserID:           req.UserID,
		ScheduledService: ScheduledService{
			IsScheduled:       true,
			ServiceCenterName: selectedCenter.Name,
			ServiceCenterID:   selectedCenter.CenterID,
			DateTime:          scheduledTime,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 6. Save to Local MongoDB 'Bookings' Collection
	_, err = bookingCollection.InsertOne(ctx, newBooking)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save booking locally"})
		return
	}

	// 7. Save Log Entry to 'Logs' Collection
	// Use incoming log ID or generate one
	logID := req.LogID
	if logID == "" {
		randNum := rand.Intn(10000)
		logID = fmt.Sprintf("LOG_%s_%04d", time.Now().Format("20060102"), randNum)
	}

	newLog := LogEntry{
		LogID:     logID,
		UserID:    req.UserID,
		VehicleID: req.VehicleID,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		LogType:   "BOOKING_CONFIRMED",
		Data: LogData{
			ConfirmationCode:  req.Data.ConfirmationCode,
			Status:            "CONFIRMED",
			ServiceCenterName: selectedCenter.Name,
			ScheduledAt:       scheduledTime,
			IsScheduled:       true,
			Action:            "ASSIGNED_CENTER_" + selectedCenter.CenterID,
		},
	}

	_, err = logsCollection.InsertOne(ctx, newLog)
	if err != nil {
		fmt.Println("Error saving log:", err)
	}

	// 8. NOTE: To "add the new Booking to the booking array of that service center",
	// you would typically send a PUT/POST request back to the external API here.
	// Since no update endpoint was provided, we assume the local DB is the record
	// or that this step is handled by another service syncing with our DB.
	// Example call (commented out):
	// updateRemoteServiceCenter(selectedCenter.CenterID, newBooking)

	// 9. Response
	c.JSON(http.StatusOK, gin.H{
		"message":            "Booking successfully scheduled",
		"assignedCenter":     selectedCenter.Name,
		"assignedCenterId":   selectedCenter.CenterID,
		"location":           selectedCenter.Location,
		"scheduledAt":        scheduledTime,
		"bookingReferenceId": newBooking.ConfirmationCode,
	})
}

// --- 6. Helper Functions ---

// fetchServiceCentersByName makes a GET request to the external API
// URL: https://admin-ey-1.onrender.com/get-center-by-name/{name}
func fetchServiceCentersByName(companyName string) ([]ServiceCenter, error) {
	// Construct the dynamic URL
	url := fmt.Sprintf("%s/get-center-by-name/%s", BaseURL, companyName)
	fmt.Println("Fetching centers from:", url)

	// Create a client with timeout
	client := http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("external API returned status: %d", resp.StatusCode)
	}

	var centers []ServiceCenter
	if err := json.NewDecoder(resp.Body).Decode(&centers); err != nil {
		return nil, err
	}

	return centers, nil
}