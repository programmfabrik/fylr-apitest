package main

import (
	"strings"
	"testing"

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
	"github.com/programmfabrik/apitest/pkg/lib/util"
)

func TestSMTPServer(t *testing.T) {

	s := Suite{
		SMTPServer: &SMTPServer{
			Addr: ":9025",
			Auth: SMTPAuth{
				Username: "root",
				Password: "admin",
			},
		},
	}

	s.StartSMTPServer()

	email := Email{
		Addr:       s.SMTPServer.Addr,
		Auth:       s.SMTPServer.Auth,
		Sender:     "sender@example.org",
		Recipients: []string{"recipient@example.net"},
		Body: "To: recipient@example.net\r\n" +
			"Subject: discount Gophers!\r\n" +
			"\r\n" +
			"This is the email body.\r\n",
	}

	auth := sasl.NewPlainClient("", email.Auth.Username, email.Auth.Password)
	msg := strings.NewReader(email.Body)
	err := smtp.SendMail(util.PolyfillLocalhost(email.Addr), auth, email.Sender, email.Recipients, msg)
	if err != nil {
		t.Fatal(err)
	}

	// Now this should fail
	auth2 := sasl.NewPlainClient("", "unknown", "object")
	err = smtp.SendMail(util.PolyfillLocalhost(email.Addr), auth2, email.Sender, email.Recipients, msg)
	if err == nil {
		t.Fatal("Invalid auth when sending email should have failed")
	}
}
