package emailwatcher

import (
	"crypto/tls"
	"time"

	logrus "github.com/Sirupsen/logrus"
	"github.com/mxk/go-imap/imap"
)

func (ew *EmailWatcher) tryConnect() error {
	le := log.WithFields(logrus.Fields{
		"server": ew.config.Server,
	})
	le.Debug("Connecting")

	var c *imap.Client
	var err error

	if ew.config.UseTLS {
		c, err = imap.DialTLS(ew.config.Server, &tls.Config{})
	} else {
		c, err = imap.Dial(ew.config.Server)
	}

	if err == nil {
		ew.imapClient = c
	} else {
		if c != nil {
			c.Logout(time.Duration(0))
		}
	}

	le.Debug("Connected")
	return err
}
