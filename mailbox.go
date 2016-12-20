package emailwatcher

import (
	"bytes"
	logrus "github.com/Sirupsen/logrus"
	"github.com/mxk/go-imap/imap"
	"net/mail"
	"time"
)

type incomingMessage struct {
	Uid          uint32
	ParsedHeader *mail.Message
	FetchAfter   <-chan time.Time
}

func (ew *EmailWatcher) trySelectInbox() error {
	if ew.config.Mailbox == "" {
		ew.config.Mailbox = "INBOX"
	}

	le := log.WithFields(logrus.Fields{
		"mailbox": ew.config.Mailbox,
	})

	le.Debug("Selecting")

	c := ew.imapClient
	_, err := imap.Wait(c.Select(ew.config.Mailbox, false))
	if err != nil {
		return err
	}

	le.Debug("Selected")
	return err
}

func (ew *EmailWatcher) watchMessages(output chan<- *incomingMessage) (watchError error) {
	defer func() {
		if watchError != nil && ew.lastError == nil {
			ew.lastError = watchError
		}
		close(output)
	}()

	le := log.WithFields(logrus.Fields{
		"mailbox": ew.config.Mailbox,
	})

	delay := ew.config.MessageCheckDelay
	if delay == 0 {
		delay = time.Duration(1) * time.Second
	}

	for ew.imapClient != nil && ew.closing != nil {
		c := ew.imapClient

		ew.clientLock.Lock()
		_, err := imap.Wait(c.Noop())
		if err != nil {
			ew.clientLock.Unlock()
			return err
		}

		// Flush new messages
		for c.Recv(0) == nil {
		}

		newCount := ew.lastSeq
		for _, msg := range c.Data {
			le.WithField("label", msg.Label).Debug("Handling")
			if msg.Label == "EXPUNGE" {
				newCount--
			}
			if msg.Label != "EXISTS" || len(msg.Fields) < 1 {
				continue
			}
			nc, ok := msg.Fields[0].(uint32)
			if ok {
				newCount = nc
			}
		}
		c.Data = nil
		if c.Mailbox.Messages > newCount && newCount == 0 {
			newCount = c.Mailbox.Messages
		}

		ew.clientLock.Unlock()

		if ew.lastSeq < newCount {
			msgs, err := ew.getRecentMessages(newCount - ew.lastSeq)
			if err != nil {
				return err
			}

			for _, msg := range msgs {
				output <- msg
			}
		}

		ew.lastSeq = newCount
		time.Sleep(delay)
	}

	return nil
}

func (ew *EmailWatcher) getRecentMessages(msgCount uint32) ([]*incomingMessage, error) {
	if msgCount == 0 {
		return nil, nil
	}

	ew.clientLock.Lock()
	defer ew.clientLock.Unlock()

	c := ew.imapClient
	le := log.WithFields(logrus.Fields{
		"mailbox": c.Mailbox.Name,
	})

	// Fetch the headers of the msgCount most recent messages
	set, _ := imap.NewSeqSet("")
	lsq := ew.lastSeq
	if lsq == 0 {
		lsq = 1
	}
	set.AddRange(lsq, ew.lastSeq+msgCount)

	le.WithFields(logrus.Fields{
		"count": msgCount,
		"start": lsq,
		"end":   ew.lastSeq + msgCount,
	}).Debug("Fetching recent messages")

	cmd, _ := c.Fetch(set, "UID", "RFC822.HEADER")

	fetchedMessages := []*incomingMessage{}
	// Process responses while the command is running
	for cmd.InProgress() {
		// Wait for the next response
		c.Recv(time.Duration(5) * time.Second)

		for _, rsp := range cmd.Data {
			mi := rsp.MessageInfo()
			if mi == nil {
				continue
			}
			header := imap.AsBytes(mi.Attrs["RFC822.HEADER"])
			msg, err := mail.ReadMessage(bytes.NewReader(header))
			if err != nil {
				return nil, err
			}
			if msg != nil {
				fetchedMessages = append(fetchedMessages, &incomingMessage{
					ParsedHeader: msg,
					Uid:          mi.UID,
				})
			}
		}
		cmd.Data = nil
	}

	// Check command completion status
	if _, err := cmd.Result(imap.OK); err != nil {
		return nil, err
	}

	c.Data = nil

	// Clear out the queue
	imap.Wait(c.Noop())
	return fetchedMessages, nil
}
