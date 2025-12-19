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
)

var client *whatsmeow.Client
var lastProcessedIDs = make(map[string]bool)

// او ٹی پی نکالنے کا فنکشن
func extractOTP(msg string) string {
	re := regexp.MustCompile(`\b\d{3,4}[-\s]?\d{3,4}\b|\b\d{4,8}\b`)
	return re.FindString(msg)
}

// نمبر ماسکنگ
func maskNumber(num string) string {
	if len(num) < 7 { return num }
	return num[:5] + "XXXX" + num[len(num)-2:]
}

// اے پی آئی مانیٹرنگ
func checkOTPs(cli *whatsmeow.Client) {
	for _, url := range Config.OTPApiURLs {
		resp, err := http.Get(url)
		if err != nil { continue }
		defer resp.Body.Close()

		var data map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil { continue }

		aaData, ok := data["aaData"].([]interface{})
		if !ok { continue }

		apiName := "API 1"
		if strings.Contains(url, "kamibroken") { apiName = "API 2" }

		for _, row := range aaData {
			r, ok := row.([]interface{})
			if !ok || len(r) < 5 { continue }

			msgID := fmt.Sprintf("%v_%v", r[2], r[0])
			if !lastProcessedIDs[msgID] {
				rawTime, _ := r[0].(string)
				countryInfo, _ := r[1].(string)
				phone, _ := r[2].(string)
				service, _ := r[3].(string)
				fullMsg, _ := r[4].(string)

				cFlag, countryWithFlag := GetCountryWithFlag(countryInfo)
				otpCode := extractOTP(fullMsg)

				messageBody := fmt.Sprintf(`
✨ *%s | %s New Message Received %s*⚡

> ⏰   *`+"`Time`"+`   •   _%s_*

> 🌍   *`+"`Country`"+`  ✓   _%s_*

  📞   *`+"`Number`"+`  √   _%s_*

> ⚙️   *`+"`Service`"+`  ©   _%s_*

  🔑   *`+"`OTP`"+`  ~   _%s_*
  
> 📋   *`+"`Join For Numbers`"+`*
  
> https://chat.whatsapp.com/EbaJKbt5J2T6pgENIeFFht

> 📩   `+"`Full Message`"+`

> `+"`%s`"+`

> Developed by Nothing Is Impossible

> `+"`🙂MR~Bunny🙂`"+` `+"`💔Um@R💔`"+` `+"`👑Mohsin~King👑`"+` 
> `+"`😎SK~SuFyAn😎`"+` `+"`😈SUDAIS~Ahmed👿`"+`
`, cFlag, strings.ToUpper(service), apiName, rawTime, countryWithFlag, maskNumber(phone), service, otpCode, fullMsg)

				for _, jidStr := range Config.OTPChannelIDs {
					jid, _ := types.ParseJID(jidStr)
					cli.SendMessage(context.Background(), jid, &waProto.Message{
						Conversation: proto.String(strings.TrimSpace(messageBody)),
					})
				}
				lastProcessedIDs[msgID] = true
			}
		}
	}
}

// ایونٹ ہینڈلر
func eventHandler(evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		msgText := v.Message.GetConversation()
		if msgText == "" { msgText = v.Message.GetExtendedTextMessage().GetText() }

		if msgText == ".id" {
			client.SendMessage(context.Background(), v.Info.Chat, &waProto.Message{
				Conversation: proto.String(fmt.Sprintf("📍 *Chat ID:* `%s`", v.Info.Chat)),
			})
		} else if msgText == ".chk" || msgText == ".check" {
			client.SendMessage(context.Background(), v.Info.Chat, &waProto.Message{
				Conversation: proto.String("🧪 *Go Bot Online* ⚡\n\nStatus: Active 🟢"),
			})
		}
	}
}

func main() {
	dbLog := waLog.Stdout("Database", "INFO", true)
	
	// فکسڈ: sqlstore.New اب 4 آرگیومنٹس لیتا ہے (context پہلا ہے)
	container, err := sqlstore.New(context.Background(), "sqlite3", "file:kami_bot.db?_foreign_keys=on", dbLog)
	if err != nil { panic(err) }
	
	// فکسڈ: GetFirstDevice اب 1 آرگیومنٹ (context) لیتا ہے
	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil { panic(err) }

	clientLog := waLog.Stdout("Client", "INFO", true)
	client = whatsmeow.NewClient(deviceStore, clientLog)
	client.AddEventHandler(eventHandler)

	if client.Store.ID == nil {
		err = client.Connect()
		if err != nil { panic(err) }
		
		fmt.Println("⏳ Requesting Pairing Code for:", Config.OwnerNumber)
		time.Sleep(3 * time.Second)
		
		// فکسڈ: PairPhone اب 5 آرگیومنٹس لیتا ہے
		code, err := client.PairPhone(context.Background(), Config.OwnerNumber, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
		if err != nil {
			fmt.Println("❌ Pairing Error:", err)
			return
		}
		fmt.Printf("\n🔑 YOUR PAIRING CODE: \033[1;32m%s\033[0m\n\n", code)
	} else {
		err = client.Connect()
		if err != nil { panic(err) }
		fmt.Println("✅ Bot Connected Successfully!")
		go func() {
			for {
				checkOTPs(client)
				time.Sleep(time.Duration(Config.Interval) * time.Second)
			}
		}()
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	client.Disconnect()
}