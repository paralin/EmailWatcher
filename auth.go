package emailwatcher

import (
	logrus "github.com/Sirupsen/logrus"
	"github.com/mxk/go-imap/imap"
)

func (ew *EmailWatcher) tryAuth() error {
	le := log.WithFields(logrus.Fields{
		"username": ew.config.Username,
	})

	le.Debug("Authenticating")

	c := ew.imapClient
	_, err := imap.Wait(c.Login(ew.config.Username, ew.config.Password))
	if err != nil {
		return err
	}

	le.Debug("Authenticated")
	c.Data = nil
	return err
}
