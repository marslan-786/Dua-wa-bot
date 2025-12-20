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
	"go.mau.fi/whatsmeow/types/events" // ڈس کنکٹ ایونٹ کے لیے
	waProto "go.mau.fi/whatsmeow/binary/proto"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var client *whatsmeow.Client
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

// --- مددگار فنکشنز ---
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
	if name == "" { return "Unknown" }
	parts := strings.Fields(strings.Split(name, "-")[0])
	if len(parts) > 0 { return parts[0] }
	return "Unknown"
}

// --- مانیٹرنگ لوپ ---
func checkOTPs(cli *whatsmeow.Client) {
	// اگر کلائنٹ کنیکٹڈ نہیں ہے تو چیک نہ کرے
	if !cli.IsConnected() || !cli.IsLoggedIn() {
		return
	}

	for i, url := range Config.OTPApiURLs {
		apiIdx := i + 1
		httpClient := &http.Client{Timeout: 5 * time.Second} // ٹائم آؤٹ تھوڑا کم کیا تاکہ تیزی سے چلے
		resp, err := httpClient.Get(url)
		if err != nil { continue }
		
		var data map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&data)
		resp.Body.Close()
		if data == nil || data["aaData"] == nil { continue }

		aaData := data["aaData"].([]interface{})
		if len(aaData) == 0 { continue }

		if isFirstRun {
			for _, row := range aaData {
				r := row.([]interface{})
				msgID := fmt.Sprintf("%v_%v", r[2], r[0])
				if !isAlreadySent(msgID) { markAsSent(msgID) }
			}
			isFirstRun = false
			return
		}

		for _, row := range aaData {
			r, ok := row.([]interface{})
			if !ok || len(r) < 5 { continue }

			rawTime := fmt.Sprintf("%v", r[0])
			countryRaw := fmt.Sprintf("%v", r[1])
			phone := fmt.Sprintf("%v", r[2])
			service := fmt.Sprintf("%v", r[3])
			fullMsg := fmt.Sprintf("%v", r[4])

			if phone == "0" || phone == "" { continue }

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
					time.Sleep(1 * time.Second) // چینل میسجز کے درمیان وقفہ کم کیا
				}
				markAsSent(msgID)
				fmt.Printf("✅ [Sent] API %d: %s\n", apiIdx, phone)
			}
		}
	}
}

// ایونٹ ہینڈلر جو ڈس کنکشن کو مانیٹر کرے گا
func handler(evt interface{}) {
	switch v := evt.(type) {
	case *events.LoggedOut:
		fmt.Println("⚠️ [Warn] Logged out from WhatsApp! Need to re-pair.")
	case *events.Disconnected:
		fmt.Println("❌ [Error] Disconnected! Bot will try to reconnect in loop.")
	}
}

func main() {
	fmt.Println("🚀 [Init] Starting Kami Bot...")
	initMongoDB()

	dbURL := os.Getenv("DATABASE_URL")
	dbType := "postgres"
	if dbURL == "" {
		dbURL = "file:kami_session.db?_foreign_keys=on"
		dbType = "sqlite3"
	}

	dbLog := waLog.Stdout("Database", "INFO", true)
	container, err := sqlstore.New(context.Background(), dbType, dbURL, dbLog)
	if err != nil { panic(err) }
	
	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil { panic(err) }

	client = whatsmeow.NewClient(deviceStore, waLog.Stdout("Client", "INFO", true))
	client.AddEventHandler(handler)

	// پہلا کنکشن
	err = client.Connect()
	if err != nil { 
		fmt.Printf("Initial connection failed: %v\n", err)
	}

	if client.Store.ID == nil {
		code, _ := client.PairPhone(context.Background(), Config.OwnerNumber, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
		fmt.Printf("\n🔑 CODE: %s\n\n", code)
	}

	// مین لوپ: ہر 3 سیکنڈ بعد چیک کرے گا اور اگر ڈس کنکٹ ہو گیا تو ری-کنکٹ کرے گا
	go func() {
		for {
			if !client.IsConnected() {
				fmt.Println("🔄 [Retry] Attempting to reconnect...")
				_ = client.Connect()
			}
			
			if client.IsLoggedIn() { 
				checkOTPs(client) 
			}
			
			time.Sleep(3 * time.Second) // اب ہر 3 سیکنڈ بعد کال ہوگی
		}
	}()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	client.Disconnect()
}
