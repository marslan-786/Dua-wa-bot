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
		"120363423562861659@newsletter",
		"120363421646654726@newsletter",
		"120363421896353312@newsletter",
	},
	OTPApiURLs: []string{
		"https://www.kamibroken.pw/api/sms?type=sms",
		"https://www.kamibroken.pw/api/sms1?type=sms",
		"https://www.kamibroken.pw/api/sms2?type=sms",
		"https://www.kamibroken.pw/api/sms3?type=sms",
	},
	Interval: 3,
}