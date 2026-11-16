// Command receiver is a demo webhook receiver. It verifies EventPulse
// signatures and can simulate failures to exercise retries.
//
//	SECRET        signing secret to verify (optional; skip verification if empty)
//	FAIL_PERCENT  percentage of requests answered with HTTP 500 (default 0)
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

func verify(secret, header string, body []byte) bool {
	var t, v1 string
	for _, part := range strings.Split(header, ",") {
		k, v, _ := strings.Cut(part, "=")
		switch k {
		case "t":
			t = v
		case "v1":
			v1 = v
		}
	}
	ts, err := strconv.ParseInt(t, 10, 64)
	if err != nil || time.Since(time.Unix(ts, 0)).Abs() > 5*time.Minute {
		return false
	}
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(t + "."))
	h.Write(body)
	return hmac.Equal([]byte(hex.EncodeToString(h.Sum(nil))), []byte(v1))
}

func main() {
	secret := os.Getenv("SECRET")
	failPct, _ := strconv.Atoi(os.Getenv("FAIL_PERCENT"))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		sig := r.Header.Get("X-EventPulse-Signature")
		status := "unverified"
		if secret != "" {
			if !verify(secret, sig, body) {
				log.Printf("REJECT bad signature delivery=%s", r.Header.Get("X-EventPulse-Delivery-Id"))
				http.Error(w, "bad signature", http.StatusUnauthorized)
				return
			}
			status = "verified"
		}
		if rand.IntN(100) < failPct {
			log.Printf("SIMULATED FAILURE delivery=%s", r.Header.Get("X-EventPulse-Delivery-Id"))
			http.Error(w, "simulated failure", http.StatusInternalServerError)
			return
		}
		log.Printf("OK (%s) event=%s delivery=%s body=%s", status, r.Header.Get("X-EventPulse-Event"), r.Header.Get("X-EventPulse-Delivery-Id"), body)
		w.WriteHeader(http.StatusOK)
	})
	log.Println("receiver listening on :9000")
	log.Fatal(http.ListenAndServe(":9000", nil))
}
