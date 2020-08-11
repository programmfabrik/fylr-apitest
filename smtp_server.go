package main

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"strings"
	"time"

	"github.com/emersion/go-smtp"
	"github.com/programmfabrik/apitest/pkg/lib/util"
	"github.com/sirupsen/logrus"
)

// SMTPAuth for SMTP login
type SMTPAuth struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// SMTPServer definition
type SMTPServer struct {
	Addr     string          `json:"addr"`
	Auth     SMTPAuth        `json:"auth"`
	Testmode bool            `json:"testmode"`
	Proxy    HTTPServerProxy `json:"proxy"`
}

// StartSMTPServer start a simple smtp server that can receive emails during the testsuite is running
func (ats *Suite) StartSMTPServer() {

	if ats.SMTPServer == nil {
		return
	}

	// If proxies are defined but the http server is not, fail
	// If it was defined, it will take care of them
	if ats.SMTPServer.Proxy != nil && len(ats.SMTPServer.Proxy) > 0 && ats.HttpServer == nil {
		logrus.Error("SMTP server needs HTTP server for its proxy")
		return
	}

	ats.smtpServer = smtp.NewServer(ats.SMTPServer)

	address := util.PolyfillLocalhost(ats.SMTPServer.Addr)
	addrSplit := strings.Split(address, ":")
	addr := fmt.Sprintf(":%s", addrSplit[1])

	ats.smtpServer.Addr = addr
	ats.smtpServer.Domain = addrSplit[0]
	ats.smtpServer.ReadTimeout = 600 * time.Second
	ats.smtpServer.WriteTimeout = 600 * time.Second
	ats.smtpServer.MaxMessageBytes = 128 * 1024 * 1024
	ats.smtpServer.MaxRecipients = 100
	ats.smtpServer.AllowInsecureAuth = true

	run := func() {
		logrus.Infof("Starting SMTP Server: %s%s", ats.smtpServer.Domain, ats.smtpServer.Addr)
		err := ats.smtpServer.ListenAndServe()
		if err != nil {
			// Error starting listener:
			logrus.Errorf("SMTP server ListenAndServe: %v", err)
			return
		}
	}

	if ats.SMTPServer.Testmode {
		// Run in foreground to test
		logrus.Infof("Testmode for SMTP Server. Listening, not running tests...")
		run()
	} else {
		go run()
	}
}

// StopSMTPServer stops the simple smtp server
func (ats *Suite) StopSMTPServer() {

	if ats.smtpServer == nil {
		return
	}

	ats.smtpServer.Close()
}

// Login handles a login command with username and password.
func (s *SMTPServer) Login(state *smtp.ConnectionState, username, password string) (smtp.Session, error) {
	if username != s.Auth.Username || password != s.Auth.Password {
		return nil, errors.New("Invalid username or password")
	}
	return &SMTPSession{Server: s}, nil
}

// AnonymousLogin requires clients to authenticate using SMTP AUTH before sending emails
func (s *SMTPServer) AnonymousLogin(state *smtp.ConnectionState) (smtp.Session, error) {
	return nil, smtp.ErrAuthRequired
}

// A SMTPSession is returned after successful login.
type SMTPSession struct {
	Server *SMTPServer
	LastMail Email
}

// Mail received
func (s *SMTPSession) Mail(from string, opts smtp.MailOptions) error {
	logrus.Debugf("Mail from: %s", from)
	s.LastMail.Sender = from
	return nil
}

// Rcpt is the recipient of the mail
func (s *SMTPSession) Rcpt(to string) error {
	logrus.Debugf("Rcpt to: %s", to)
	s.LastMail.Recipients = append(s.LastMail.Recipients, to)
	return nil
}

// Data received from mail
func (s *SMTPSession) Data(r io.Reader) error {
	
	b, err := ioutil.ReadAll(r)
	if err != nil {
		return err
	}
	s.LastMail.Body = string(b)
	// Aparently an extra single new line is inserted at the end somehow?
	s.LastMail.Body = s.LastMail.Body[:len(s.LastMail.Body)-1] 

	logrus.Debugf("Data: %s", string(b))

	reqData := HTTPServerProxyStoreRequestData{
		Method: "MAIL",
		Body: s.LastMail,
	}
	resData := HTTPServerProxyStoreResponseData{}

	// TODO: Way to organize them? By sender? Would we know it?
	for p := range s.Server.Proxy {
		s.Server.Proxy.addEntryToStore(p,HTTPServerProxyStoreDataEntry{0, reqData, resData})
	}

	return nil
}

// Reset a session
func (s *SMTPSession) Reset() {}

// Logout a session
func (s *SMTPSession) Logout() error {
	return nil
}

// Email definition
type Email struct {
	Addr       string   `json:"addr"`
	Auth       SMTPAuth `json:"auth"`
	Sender     string   `json:"sender"`
	Recipients []string `json:"recipients"`
	Body       string   `json:"body"`
}
