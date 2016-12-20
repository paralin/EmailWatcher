package emailwatcher

import (
	"bytes"
	"io"
	"sync"
	"time"

	logrus "github.com/Sirupsen/logrus"
	"github.com/mxk/go-imap/imap"
)

// Watches an IMAP inbox for incoming Steam verification messages.
type EmailWatcher struct {
	config    *EmailWatcherConfig
	closing   chan chan error
	lastError error

	imapClient *imap.Client
	clientLock sync.Mutex

	lastSeq uint32
}

type EmailWatcherConfig struct {
	Server            string
	Username          string
	Password          string
	UseTLS            bool
	RetryConn         bool
	Mailbox           string
	Matchers          []MessageMatcher
	MessageCheckDelay time.Duration
}

func NewEmailWatcher(config *EmailWatcherConfig) *EmailWatcher {
	return &EmailWatcher{
		config:  config,
		closing: nil,
	}
}

func (ew *EmailWatcher) Start() {
	if ew.closing != nil {
		return
	}

	ew.closing = make(chan chan error)
	go ew.updateLoop()
}

func (ew *EmailWatcher) updateOnce() error {
	if err := ew.tryConnect(); err != nil {
		return err
	}

	if err := ew.tryAuth(); err != nil {
		return err
	}

	if err := ew.trySelectInbox(); err != nil {
		return err
	}

	msgChan := make(chan *incomingMessage, 10)
	go ew.watchMessages(msgChan)

	for {
		select {
		case msg, ok := <-msgChan:
			if !ok {
				return ew.lastError
			}
			if msg.FetchAfter != nil {
				<-msg.FetchAfter
				msg.FetchAfter = nil
			}
			for _, handler := range ew.config.Matchers {
				if handler.HeaderMatches(&msg.ParsedHeader.Header) {
					body, err := ew.fetchMessageBody(msg.Uid)
					if err != nil {
						log.WithField("message", msg.Uid).Warn(err.Error())
						break
					}
					msg.ParsedHeader.Body = io.Reader(bytes.NewReader(body))
					ew.clientLock.Lock()
					handler.ProcessMessage(msg.ParsedHeader, msg.Uid, ew.imapClient)
					ew.clientLock.Unlock()
					break
				}
			}
		case cc := <-ew.closing:
			cc <- ew.lastError
			ew.closing = nil
			return nil
		}
	}
}

func (ew *EmailWatcher) releaseAll() {
	if ew.imapClient != nil {
		log.Debug("Closing imap client")
		ew.imapClient.Logout(time.Duration(1) * time.Second)
		ew.imapClient = nil
	}
}

func (ew *EmailWatcher) updateLoop() {
	log.Debug("Starting...")
	defer func() {
		log.Debug("Exiting...")
		ew.closing = nil
		ew.releaseAll()
	}()

	for {
		err := ew.updateOnce()

		if err != nil {
			ew.lastError = err
			log.WithFields(logrus.Fields{
				"error": err.Error(),
			}).Error("Error")
		}

		if !ew.config.RetryConn || ew.closing == nil || ew.shouldExit() {
			return
		}

		ew.releaseAll()
		select {
		case cc := <-ew.closing:
			cc <- ew.lastError
			return
		case <-time.After(time.Duration(1) * time.Second):
		}
	}
}

func (ew *EmailWatcher) shouldExit() bool {
	select {
	case cc := <-ew.closing:
		cc <- ew.lastError
		return true
	default:
	}
	return false
}

func (ew *EmailWatcher) Close() error {
	if ew.closing == nil {
		return ew.lastError
	}

	errc := make(chan error)
	ew.closing <- errc
	defer func() {
		ew.closing = nil
	}()
	return <-errc
}
