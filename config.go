package main

var Config = struct {
	OwnerNumber   string
	BotName       string
	OTPChannelIDs []string
	OTPApiURLs    []string
	Interval      int
}{
	OwnerNumber: "923027665767",
	BotName:     "Kami OTP Monitor",
	OTPChannelIDs: []string{
		"120363423779796506@newsletter",
	},
	OTPApiURLs: []string{
		"https://dua-nodejs-api-production.up.railway.app/api?type=sms",
		"https://dua-api-go-production.up.railway.app/d-group/sms",
		"https://dua-api-go-production.up.railway.app/mait/sms",
	},
	Interval: 4,
}
