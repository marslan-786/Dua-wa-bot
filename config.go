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
		"https://kamina-otp.up.railway.app/mait/sms",
		"https://kamina-otp.up.railway.app/d-group/sms",
		"https://web-production-b717.up.railway.app/api?type=sms",
		"https://kamina-otp.up.railway.app/npm-neon/sms",
	},
	Interval: 3,
}