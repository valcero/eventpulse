package dispatcher

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"
)

// SignatureHeader carries "t=<unix>,v1=<hex>" where v1 = HMAC-SHA256(secret, "<t>.<body>").
// Receivers should recompute the MAC, compare in constant time, and reject
// timestamps outside a tolerance window to prevent replay.
const SignatureHeader = "X-EventPulse-Signature"

// Sign returns the header value for body signed at ts.
func Sign(secret string, ts time.Time, body []byte) string {
	t := strconv.FormatInt(ts.Unix(), 10)
	return "t=" + t + ",v1=" + mac(secret, t, body)
}

func mac(secret, t string, body []byte) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(t))
	h.Write([]byte("."))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}
