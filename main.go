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

// --- اے پی آئی مانیٹرنگ اور چینل سینڈنگ ---
func checkOTPs(cli *whatsmeow.Client) {
	fmt.Println("🔍 [Monitor] Checking APIs for new activity...")
	for _, url := range Config.OTPApiURLs {
		fmt.Printf("🌐 [API] Requesting: %s\n", url)
		resp, err := http.Get(url)
		if err != nil {
			fmt.Printf("❌ [API ERROR]: %v\n", err)
			continue
		}
		defer resp.Body.Close()

		var data map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&data)

		aaData, ok := data["aaData"].([]interface{})
		if !ok { continue }

		apiName := "API 1"
		if strings.Contains(url, "kamibroken") { apiName = "API 2" }

		for _, row := range aaData {
			r, ok := row.([]interface{})
			if !ok || len(r) < 5 { continue }

			msgID := fmt.Sprintf("%v_%v", r[2], r[0])
			if !lastProcessedIDs[msgID] {
				fmt.Printf("📩 [New Msg] Found message for number: %v\n", r[2])
				
				rawTime, _ := r[0].(string)
				countryInfo, _ := r[1].(string)
				phone, _ := r[2].(string)
				service, _ := r[3].(string)
				fullMsg, _ := r[4].(string)

				cFlag, countryWithFlag := GetCountryWithFlag(countryInfo)
				otpCode := extractOTP(fullMsg)

				messageBody := fmt.Sprintf(`✨ *%s | %s New Message Received %s*⚡
> ⏰ Time: _%s_
> 🌍 Country: _%s_
> 📞 Number: _%s_
> ⚙️ Service: _%s_
> 🔑 OTP: *%s*

📩 Full Message:
"%s"

_Developed by Nothing Is Impossible_`, cFlag, strings.ToUpper(service), apiName, rawTime, countryWithFlag, maskNumber(phone), service, otpCode, fullMsg)

				for _, jidStr := range Config.OTPChannelIDs {
					jid, _ := types.ParseJID(jidStr)
					fmt.Printf("📤 [Channel] Sending to: %s\n", jidStr)
					
					_, err := cli.SendMessage(context.Background(), jid, &waProto.Message{
						Conversation: proto.String(strings.TrimSpace(messageBody)),
					})
					if err != nil {
						fmt.Printf("❌ [Send Error] Channel %s failed: %v\n", jidStr, err)
					} else {
						fmt.Printf("✅ [Success] Sent to channel %s\n", jidStr)
					}
				}
				lastProcessedIDs[msgID] = true
			}
		}
	}
}

// --- بٹن ٹیسٹنگ لاجک ---
func sendTestButtons(cli *whatsmeow.Client, chat types.JID) {
	fmt.Printf("🛠 [Test] Building buttons for %s...\n", chat)

	// 1. لسٹ میسج (3-Line Style)
	listMsg := &waProto.Message{
		ListMessage: &waProto.ListMessage{
			Title:       proto.String("Kami Bot Menu"),
			Description: proto.String("Please select an option below"),
			ButtonText:  proto.String("Open Menu"),
			ListType:    waProto.ListMessage_SINGLE_SELECT.Enum(),
			Sections: []*waProto.ListMessage_Section{
				{
					Title: proto.String("Commands"),
					Rows: []*waProto.ListMessage_Row{
						{Title: proto.String("Get ID"), RowId: proto.String("id_1")},
						{Title: proto.String("Check Status"), RowId: proto.String("st_1")},
					},
				},
			},
		},
	}

	// 2. انٹرایکٹو بٹنز (Native Flow - Copy Code Style)
	interactiveMsg := &waProto.Message{
		ViewOnceMessage: &waProto.ViewOnceMessage{
			Message: &waProto.Message{
				InteractiveMessage: &waProto.InteractiveMessage{
					Body: &waProto.InteractiveMessage_Body{
						Text: proto.String("⚡ *OTP Received* \n\nClick the button below to copy your code."),
					},
					InteractiveMessageConfig: &waProto.InteractiveMessage_NativeFlowMessage_{
						NativeFlowMessage: &waProto.InteractiveMessage_NativeFlowMessage{
							Buttons: []*waProto.InteractiveMessage_NativeFlowMessage_Button{
								{
									Name: proto.String("cta_copy"),
									ButtonParamsJson: proto.String(`{"display_text":"Copy OTP","id":"123","copy_code":"456-789"}`),
								},
								{
									Name: proto.String("cta_url"),
									ButtonParamsJson: proto.String(`{"display_text":"Join Support","url":"https://chat.whatsapp.com/EbaJKbt5J2T6pgENIeFFht"}`),
								},
							},
						},
					},
				},
			},
		},
	}

	// باری باری ٹیسٹ بھیجنا
	styles := []struct {
		name string
		msg  *waProto.Message
	}{
		{"List Message", listMsg},
		{"Native Flow Buttons", interactiveMsg},
	}

	for _, s := range styles {
		fmt.Printf("🚀 [Test] Attempting Style: %s\n", s.name)
		resp, err := cli.SendMessage(context.Background(), chat, s.msg)
		if err != nil {
			fmt.Printf("❌ [%s ERROR]: %v\n", s.name, err)
		} else {
			fmt.Printf("✅ [%s SUCCESS]: ID %s\n", s.name, resp.ID)
		}
	}
}

// --- ایونٹ ہینڈلر ---
func eventHandler(evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		msgText := v.Message.GetConversation()
		if msgText == "" { msgText = v.Message.GetExtendedTextMessage().GetText() }

		if msgText == ".id" {
			fmt.Printf("📩 [Command] .id from %s\n", v.Info.Chat)
			client.SendMessage(context.Background(), v.Info.Chat, &waProto.Message{
				Conversation: proto.String(fmt.Sprintf("📍 Chat ID: `%s`", v.Info.Chat)),
			})
		} else if msgText == ".chk" || msgText == ".check" {
			sendTestButtons(client, v.Info.Chat)
		}
	}
}

func main() {
	fmt.Println("🚀 [Init] Starting Kami OTP Bot...")
	
	dbLog := waLog.Stdout("Database", "INFO", true)
	container, err := sqlstore.New(context.Background(), "sqlite3", "file:kami_bot.db?_foreign_keys=on", dbLog)
	if err != nil { panic(err) }
	
	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil { panic(err) }

	client = whatsmeow.NewClient(deviceStore, waLog.Stdout("Client", "INFO", true))
	client.AddEventHandler(eventHandler)

	if client.Store.ID == nil {
		err = client.Connect()
		if err != nil { panic(err) }
		fmt.Println("⏳ [Auth] Requesting Pairing Code...")
		code, err := client.PairPhone(context.Background(), Config.OwnerNumber, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
		if err != nil { fmt.Printf("❌ [Error] %v\n", err); return }
		fmt.Printf("\n🔑 PAIRING CODE: %s\n\n", code)
	} else {
		err = client.Connect()
		if err != nil { panic(err) }
		fmt.Println("✅ [Ready] Connected to WhatsApp!")
		go func() {
			fmt.Println("⏰ [Loop] Monitoring started.")
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