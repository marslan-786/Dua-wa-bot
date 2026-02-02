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
		"https://api-kami-nodejs-production-a53d.up.railway.app/api?type=sms",
		"https://kami-otp.up.railway.app/d-group/sms",
		"https://kami-otp.up.railway.app/npm-neon/sms",
		"https://kami-otp.up.railway.app/mait/sms",
		"https://api-node-js-new-production-b09a.up.railway.app/api?type=sms",
	},
	Interval: 4,
}
