package emailwatcher

import (
	"errors"
	logrus "github.com/Sirupsen/logrus"
	"github.com/mxk/go-imap/imap"
	"net/mail"
)

type MessageMatcher interface {
	// Check if the header matches.
	HeaderMatches(head *mail.Header) bool

	// Process the message.
	ProcessMessage(message *mail.Message, client *imap.Client)
}

func (ew *EmailWatcher) fetchMessageBody(uid uint32) ([]byte, error) {
	ew.clientLock.Lock()
	defer ew.clientLock.Unlock()

	c := ew.imapClient
	le := log.WithFields(logrus.Fields{
		"mailbox": c.Mailbox.Name,
		"message": uid,
	})
	le.Debug("Fetching body")

	set, _ := imap.NewSeqSet("")
	set.AddNum(uid)

	cmd, err := c.UIDFetch(set, "RFC822.TEXT")
	if err != nil {
		return nil, err
	}

	for cmd.InProgress() {
		c.Recv(-1)

		for _, rsp := range cmd.Data {
			mi := rsp.MessageInfo()
			if mi == nil {
				continue
			}

			body, ok := mi.Attrs["RFC822.TEXT"]
			if !ok {
				le.WithField("data", cmd.Data[0].String()).Warn("No body data")
				return nil, errors.New("No body data")
			}

			return imap.AsBytes(body), nil
		}
	}

	le.Warn("No message info")
	return nil, errors.New("No message info")

}
