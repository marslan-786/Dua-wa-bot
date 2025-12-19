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

	_ "github.com/mattn/go-sqlite3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	// MongoDB Drivers
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var client *whatsmeow.Client
var mongoColl *mongo.Collection
var isFirstRun = true

// --- مونگو ڈی بی کنکشن ---
func initMongoDB() {
	uri := "mongodb://mongo:AEvrikOWlrmJCQrDTQgfGtqLlwhwLuAA@crossover.proxy.rlwy.net:29609"
	fmt.Println("🍃 [DB] Connecting to MongoDB...")
	
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mClient, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil { panic(err) }

	// ڈیٹا بیس اور کلیکشن سلیکٹ کریں
	mongoColl = mClient.Database("kami_otp_db").Collection("sent_otps")
	fmt.Println("✅ [DB] MongoDB Connected Successfully!")
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
	_, _ = mongoColl.InsertOne(ctx, bson.M{"msg_id": id, "created_at": time.Now()})
}

// --- مددگار فنکشنز ---
func extractOTP(msg string) string {
	re := regexp.MustCompile(`\b\d{3,4}[-\s]?\d{3,4}\b|\b\d{4,8}\b`)
	return re.FindString(msg)
}

func maskNumber(num string) string {
	if len(num) < 7 { return num }
	return num[:5] + "XXXX" + num[len(num)-2:]
}

func cleanCountryName(name string) string {
	firstPart := strings.Split(name, "-")[0]
	return strings.Fields(firstPart)[0]
}

// --- مین مانیٹرنگ لوپ ---
func checkOTPs(cli *whatsmeow.Client) {
	if cli == nil || !cli.IsConnected() { return }

	for i, url := range Config.OTPApiURLs {
		apiIdx := i + 1
		httpClient := &http.Client{Timeout: 8 * time.Second}
		resp, err := httpClient.Get(url)
		if err != nil {
			fmt.Printf("⚠️ [SKIP] API %d Timeout\n", apiIdx)
			continue
		}

		var data map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&data)
		resp.Body.Close()
		if data == nil || data["aaData"] == nil { continue }

		aaData := data["aaData"].([]interface{})
		if len(aaData) == 0 { continue }

		apiName := "API-Server"
		if strings.Contains(url, "kamibroken") { apiName = "Kami-Broken" }

		// فرسٹ رن لاجک: پرانے میسجز کو ڈیٹا بیس میں ڈالیں
		if isFirstRun {
			fmt.Printf("🚀 [First Run] Syncing %d old records to MongoDB...\n", len(aaData))
			for _, row := range aaData {
				r := row.([]interface{})
				msgID := fmt.Sprintf("%v_%v", r[2], r[0])
				if !isAlreadySent(msgID) { markAsSent(msgID) }
			}
			// تازہ ترین ایک میسج دوبارہ اوپن کریں تاکہ وہ سینڈ ہو
			latestRow := aaData[0].([]interface{})
			latestID := fmt.Sprintf("%v_%v", latestRow[2], latestRow[0])
			mongoColl.DeleteOne(context.Background(), bson.M{"msg_id": latestID})
			isFirstRun = false
		}

		for _, row := range aaData {
			r, ok := row.([]interface{})
			if !ok || len(r) < 5 { continue }

			msgID := fmt.Sprintf("%v_%v", r[2], r[0])

			if !isAlreadySent(msgID) {
				fmt.Printf("📩 [New] API %d: Forwarding OTP for %v\n", apiIdx, r[2])
				
				rawTime, _ := r[0].(string)
				countryRaw, _ := r[1].(string)
				phone, _ := r[2].(string)
				service, _ := r[3].(string)
				fullMsg, _ := r[4].(string)

				cleanCountry := cleanCountryName(countryRaw)
				cFlag, _ := GetCountryWithFlag(cleanCountry)
				otpCode := extractOTP(fullMsg)
				flatMsg := strings.ReplaceAll(strings.ReplaceAll(fullMsg, "\n", " "), "\r", "")

				messageBody := fmt.Sprintf(`✨ *%s | %s Message %d*⚡

> ⏰   *`+"`Time`"+`   •   _%s_*

> 🌍   *`+"`Country`"+`  ✓   _%s_*

  📞   *`+"`Number`"+`  √   _%s_*

> ⚙️   *`+"`Service`"+`  ©   _%s_*

  🔑   *`+"`OTP`"+`  ~   _%s_*

> 📡   *`+"`API`"+`  •   _%s_*
  
> 📋   *`+"`Join For Numbers`"+`*
  
> https://chat.whatsapp.com/EbaJKbt5J2T6pgENIeFFht

📩 Full Msg:
> %s

> Developed by Nothing Is Impossible`, cFlag, strings.ToUpper(service), apiIdx, rawTime, cFlag + " " + cleanCountry, maskNumber(phone), service, otpCode, apiName, flatMsg)

				for _, jidStr := range Config.OTPChannelIDs {
					jid, _ := types.ParseJID(jidStr)
					_, err := cli.SendMessage(context.Background(), jid, &waProto.Message{
						Conversation: proto.String(strings.TrimSpace(messageBody)),
					})
					if err != nil {
						fmt.Printf("❌ [Send Error] API %d to %s: %v\n", apiIdx, jidStr, err)
					}
				}
				markAsSent(msgID) // اب مونگو میں محفوظ کر لو
			}
		}
	}
}

func eventHandler(evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		msgText := v.Message.GetConversation()
		if msgText == "" { msgText = v.Message.GetExtendedTextMessage().GetText() }

		if msgText == ".id" {
			client.SendMessage(context.Background(), v.Info.Chat, &waProto.Message{
				Conversation: proto.String(fmt.Sprintf("📍 Chat ID: `%s`", v.Info.Chat)),
			})
		}
	}
}

func main() {
	fmt.Println("🚀 [Boot] Starting Kami OTP Bot...")
	initMongoDB() // مونگو ڈی بی شروع کریں

	dbLog := waLog.Stdout("Database", "INFO", true)
	// واٹس ایپ سیشن کے لیے SQLite ہی رہے گا کیونکہ لائبریری مونگو کو سیشن کے لیے سپورٹ نہیں کرتی
	container, err := sqlstore.New(context.Background(), "sqlite3", "file:kami_session.db?_foreign_keys=on", dbLog)
	if err != nil { panic(err) }
	
	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil { panic(err) }

	client = whatsmeow.NewClient(deviceStore, waLog.Stdout("Client", "INFO", true))
	client.AddEventHandler(eventHandler)

	err = client.Connect()
	if err != nil { panic(err) }

	if client.Store.ID == nil {
		fmt.Println("⏳ [Auth] Scan pairing code...")
		time.Sleep(3 * time.Second)
		code, err := client.PairPhone(context.Background(), Config.OwnerNumber, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
		if err != nil { fmt.Printf("❌ [Error] %v\n", err); return }
		fmt.Printf("\n🔑 CODE: %s\n\n", code)
	}

	go func() {
		for {
			if client.IsLoggedIn() {
				checkOTPs(client)
			}
			time.Sleep(time.Duration(Config.Interval) * time.Second)
		}
	}()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	client.Disconnect()
}