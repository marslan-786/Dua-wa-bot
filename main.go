package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var client *whatsmeow.Client
var container *sqlstore.Container
var mongoColl *mongo.Collection
var isFirstRun = true

// --- MongoDB Setup ---
func initMongoDB() {
	uri := "mongodb://mongo:AEvrikOWlrmJCQrDTQgfGtqLlwhwLuAA@crossover.proxy.rlwy.net:29609"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	mClient, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		panic(err)
	}
	mongoColl = mClient.Database("kami_otp_db").Collection("sent_otps")
}

func isAlreadySent(id string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var result bson.M
	err := mongoColl.FindOne(ctx, bson.M{"msg_id": id}).Decode(&result)
	return err == nil
}

func markAsSent(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _ = mongoColl.InsertOne(ctx, bson.M{"msg_id": id, "at": time.Now()})
}

// --- Helper Functions ---
func extractOTP(msg string) string {
	re := regexp.MustCompile(`\b\d{3,4}[-\s]?\d{3,4}\b|\b\d{4,8}\b`)
	return re.FindString(msg)
}

func maskPhoneNumber(phone string) string {
	if len(phone) < 6 {
		return phone
	}
	return fmt.Sprintf("%s•••%s", phone[:3], phone[len(phone)-2:])
}

func cleanCountryName(name string) string {
	if name == "" {
		return "Unknown"
	}
	parts := strings.Fields(strings.Split(name, "-")[0])
	if len(parts) > 0 {
		return parts[0]
	}
	return "Unknown"
}

// --- Monitoring Loop ---
func checkOTPs(cli *whatsmeow.Client) {
	if !cli.IsConnected() || !cli.IsLoggedIn() {
		return
	}

	for i, url := range Config.OTPApiURLs {
		apiIdx := i + 1
		httpClient := &http.Client{Timeout: 5 * time.Second}
		resp, err := httpClient.Get(url)
		if err != nil {
			continue
		}

		var data map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&data)
		resp.Body.Close()
		if data == nil || data["aaData"] == nil {
			continue
		}

		aaData := data["aaData"].([]interface{})
		if len(aaData) == 0 {
			continue
		}

		if isFirstRun {
			for _, row := range aaData {
				r := row.([]interface{})
				msgID := fmt.Sprintf("%v_%v", r[2], r[0])
				if !isAlreadySent(msgID) {
					markAsSent(msgID)
				}
			}
			isFirstRun = false
			return
		}

		for _, row := range aaData {
			r, ok := row.([]interface{})
			if !ok || len(r) < 5 {
				continue
			}

			rawTime := fmt.Sprintf("%v", r[0])
			countryRaw := fmt.Sprintf("%v", r[1])
			phone := fmt.Sprintf("%v", r[2])
			service := fmt.Sprintf("%v", r[3])
			fullMsg := fmt.Sprintf("%v", r[4])

			if phone == "0" || phone == "" {
				continue
			}

			msgID := fmt.Sprintf("%v_%v", phone, rawTime)

			if !isAlreadySent(msgID) {
				cleanCountry := cleanCountryName(countryRaw)
				cFlag, _ := GetCountryWithFlag(cleanCountry)
				otpCode := extractOTP(fullMsg)
				maskedPhone := maskPhoneNumber(phone)
				flatMsg := strings.ReplaceAll(strings.ReplaceAll(fullMsg, "\n", " "), "\r", "")

				messageBody := fmt.Sprintf("✨ *%s | %s Message %d* ⚡\n\n"+
					"> *Time:* %s\n"+
					"> *Country:* %s %s\n"+
					"> *Number:* *%s*\n"+
					"> *Service:* %s\n"+
					"> *OTP:* *%s*\n\n"+
					"> *Join For Numbers:* \n"+
					"> https://chat.whatsapp.com/EbaJKbt5J2T6pgENIeFFht\n"+
					"> https://chat.whatsapp.com/L0Qk2ifxRFU3fduGA45osD\n\n"+
					"*Full Message:*\n"+
					"%s\n\n"+
					"> © Developed by Nothing Is Impossible",
					cFlag, strings.ToUpper(service), apiIdx,
					rawTime, cFlag, cleanCountry, maskedPhone, service, otpCode, flatMsg)

				for _, jidStr := range Config.OTPChannelIDs {
					jid, _ := types.ParseJID(jidStr)
					cli.SendMessage(context.Background(), jid, &waProto.Message{
						Conversation: proto.String(strings.TrimSpace(messageBody)),
					})
					time.Sleep(1 * time.Second)
				}
				markAsSent(msgID)
				fmt.Printf("✅ [Sent] API %d: %s\n", apiIdx, phone)
			}
		}
	}
}

// Event Handler
func handler(evt interface{}) {
	switch evt.(type) {
	case *events.LoggedOut:
		fmt.Println("⚠️ [Warn] Logged out from WhatsApp!")
	case *events.Disconnected:
		fmt.Println("❌ [Error] Disconnected! Reconnecting...")
	case *events.Connected:
		fmt.Println("✅ [Info] Connected to WhatsApp")
	}
}

// ================= API ENDPOINTS =================

func handlePairAPI(w http.ResponseWriter, r *http.Request) {
	// Extract number from URL: /link/pair/923027665767
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 {
		http.Error(w, `{"error":"Invalid URL format. Use: /link/pair/NUMBER"}`, 400)
		return
	}

	number := strings.TrimSpace(parts[3])
	number = strings.ReplaceAll(number, "+", "")
	number = strings.ReplaceAll(number, " ", "")
	number = strings.ReplaceAll(number, "-", "")

	if len(number) < 10 || len(number) > 15 {
		http.Error(w, `{"error":"Invalid phone number"}`, 400)
		return
	}

	fmt.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("📱 PAIRING REQUEST: %s\n", number)

	// Disconnect current session
	if client != nil && client.IsConnected() {
		fmt.Println("🔄 Disconnecting old session...")
		client.Disconnect()
		time.Sleep(2 * time.Second)
	}

	// Create new device
	newDevice := container.NewDevice()
	tempClient := whatsmeow.NewClient(newDevice, waLog.Stdout("Pairing", "INFO", true))
	tempClient.AddEventHandler(handler)

	// Connect
	err := tempClient.Connect()
	if err != nil {
		fmt.Printf("❌ Connection failed: %v\n", err)
		http.Error(w, fmt.Sprintf(`{"error":"Connection failed: %v"}`, err), 500)
		return
	}

	// Wait for stable connection
	time.Sleep(3 * time.Second)

	// Generate pairing code
	code, err := tempClient.PairPhone(
		context.Background(),
		number,
		true,
		whatsmeow.PairClientChrome,
		"Chrome (Linux)",
	)

	if err != nil {
		fmt.Printf("❌ Pairing failed: %v\n", err)
		tempClient.Disconnect()
		http.Error(w, fmt.Sprintf(`{"error":"Pairing failed: %v"}`, err), 500)
		return
	}

	fmt.Printf("✅ Code generated: %s\n", code)
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

	// Watch for successful pairing
	go func() {
		for i := 0; i < 60; i++ {
			time.Sleep(1 * time.Second)
			if tempClient.Store.ID != nil {
				fmt.Println("✅ Pairing successful!")
				client = tempClient
				return
			}
		}
		fmt.Println("❌ Pairing timeout")
		tempClient.Disconnect()
	}()

	// Return response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"success": "true",
		"code":    code,
		"number":  number,
	})
}

func handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	fmt.Println("\n🗑️ DELETE SESSION REQUEST")

	if client != nil && client.IsConnected() {
		client.Disconnect()
		fmt.Println("✅ Client disconnected")
	}

	// Delete all devices from DB
	devices, _ := container.GetAllDevices(context.Background())
	for _, device := range devices {
		err := device.Delete(context.Background())
		if err != nil {
			fmt.Printf("⚠️ Failed to delete device: %v\n", err)
		}
	}

	fmt.Println("✅ All sessions deleted")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"success": "true",
		"message": "Session deleted successfully",
	})
}

func main() {
	fmt.Println("🚀 [Init] Starting Kami Bot...")
	initMongoDB()

	// Database setup
	dbURL := os.Getenv("DATABASE_URL")
	dbType := "postgres"
	if dbURL == "" {
		dbURL = "file:kami_session.db?_foreign_keys=on"
		dbType = "sqlite3"
	}

	dbLog := waLog.Stdout("Database", "INFO", true)
	var err error
	container, err = sqlstore.New(context.Background(), dbType, dbURL, dbLog)
	if err != nil {
		panic(err)
	}

	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil {
		panic(err)
	}

	client = whatsmeow.NewClient(deviceStore, waLog.Stdout("Client", "INFO", true))
	client.AddEventHandler(handler)

	// Try to connect if session exists
	if client.Store.ID != nil {
		err = client.Connect()
		if err != nil {
			fmt.Printf("⚠️ Connection failed: %v\n", err)
		} else {
			fmt.Println("✅ Session restored")
		}
	} else {
		fmt.Println("⏳ No session - use /link/pair/NUMBER to pair")
	}

	// Start OTP monitoring
	go func() {
		for {
			if !client.IsConnected() && client.Store.ID != nil {
				fmt.Println("🔄 Reconnecting...")
				_ = client.Connect()
			}

			if client.IsLoggedIn() {
				checkOTPs(client)
			}

			time.Sleep(3 * time.Second)
		}
	}()

	// ================= HTTP SERVER =================
	http.HandleFunc("/link/pair/", handlePairAPI)
	http.HandleFunc("/link/delete", handleDeleteSession)

	// Get port from environment (Railway sets PORT)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	go func() {
		fmt.Printf("🌐 API Server running on port %s\n", port)
		fmt.Printf("📱 Pair: http://localhost:%s/link/pair/NUMBER\n", port)
		fmt.Printf("🗑️ Delete: http://localhost:%s/link/delete\n", port)
		if err := http.ListenAndServe(":"+port, nil); err != nil {
			fmt.Printf("❌ Server error: %v\n", err)
		}
	}()

	// Graceful shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	fmt.Println("\n🛑 Shutting down...")
	if client != nil {
		client.Disconnect()
	}
}