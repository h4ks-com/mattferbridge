package bsshchat

import (
	"bufio"
	"io"
	"regexp"
	"strings"

	"github.com/42wim/matterbridge/bridge"
	"github.com/42wim/matterbridge/bridge/config"
	"github.com/42wim/matterbridge/bridge/helper"
	"github.com/shazow/ssh-chat/sshd"
)

type Bsshchat struct {
	r *bufio.Scanner
	w io.WriteCloser
	*bridge.Config
}

func New(cfg *bridge.Config) bridge.Bridger {
	return &Bsshchat{Config: cfg}
}

func (b *Bsshchat) Connect() error {
	b.Log.Infof("Connecting %s", b.GetString("Server"))

	// connHandler will be called by 'sshd.ConnectShell()' below
	// once the connection is established in order to handle it.
	connErr := make(chan error, 1) // Needs to be buffered.
	connSignal := make(chan struct{})
	connHandler := func(r io.Reader, w io.WriteCloser) error {
		b.r = bufio.NewScanner(r)
		b.r.Scan()
		b.w = w
		if _, err := b.w.Write([]byte("/theme mono\r\n/quiet\r\n")); err != nil {
			return err
		}
		close(connSignal) // Connection is established so we can signal the success.
		return b.handleSSHChat()
	}

	go func() {
		// As a successful connection will result in this returning after the Connection
		// method has already returned point we NEED to have a buffered channel to still
		// be able to write.
		connErr <- sshd.ConnectShell(b.GetString("Server"), b.GetString("Nick"), connHandler)
	}()

	select {
	case err := <-connErr:
		b.Log.Error("Connection failed")
		return err
	case <-connSignal:
	}
	b.Log.Info("Connection succeeded")
	return nil
}

func (b *Bsshchat) Disconnect() error {
	return nil
}

func (b *Bsshchat) JoinChannel(channel config.ChannelInfo) error {
	return nil
}

func (b *Bsshchat) Send(msg config.Message) (string, error) {
	// ignore delete messages
	if msg.Event == config.EventMsgDelete {
		return "", nil
	}
	if b.GetBool("Debug") {
		b.Log.Debugf("=> Receiving %#v", msg)
	}
	if msg.Extra != nil {
		for _, rmsg := range helper.HandleExtra(&msg, b.General) {
			// Format actions with asterisks for extra messages
			if rmsg.Event == config.EventUserAction {
				originalNick := b.extractOriginalNick(rmsg.Username)
				text := originalNick + " *" + rmsg.Text + "*"
				if _, err := b.w.Write([]byte(text + "\r\n")); err != nil {
					b.Log.Errorf("Could not send extra action message: %#v", err)
				}
			} else {
				if _, err := b.w.Write([]byte(rmsg.Username + rmsg.Text + "\r\n")); err != nil {
					b.Log.Errorf("Could not send extra message: %#v", err)
				}
			}
		}
		if len(msg.Extra["file"]) > 0 {
			return b.handleUploadFile(&msg)
		}
	}

	// Format main message based on event type
	var messageText string
	if msg.Event == config.EventUserAction {
		// Extract original nickname for action formatting
		originalNick := b.extractOriginalNick(msg.Username)
		// Format action messages with asterisks around action only: username *action*
		messageText = originalNick + " *" + msg.Text + "*"
		if b.GetBool("Debug") {
			b.Log.Debugf("=> Sending action message: %s (original nick: %s)", messageText, originalNick)
		}
	} else {
		// Regular message format: username message
		messageText = msg.Username + msg.Text
	}

	_, err := b.w.Write([]byte(messageText + "\r\n"))
	return "", err
}

/*
func (b *Bsshchat) sshchatKeepAlive() chan bool {
	done := make(chan bool)
	go func() {
		ticker := time.NewTicker(90 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				b.Log.Debugf("PING")
				err := b.xc.PingC2S("", "")
				if err != nil {
					b.Log.Debugf("PING failed %#v", err)
				}
			case <-done:
				return
			}
		}
	}()
	return done
}
*/

func stripPrompt(s string) string {
	pos := strings.LastIndex(s, "\033[K")
	if pos < 0 {
		return s
	}
	return s[pos+3:]
}

// sshChatParse extracts a chat message from one terminal line written by the
// ssh-chat server, or returns nil if the line should not be relayed. Lines
// that reach steady-state come from the bot's own terminal rendering — the
// server clears the prompt with `\x1b[K` and then writes the message body, so
// we anchor parsing on that escape. Returned Message has Account left empty
// for the caller to fill.
func sshChatParse(text, botNick string) *config.Message {
	if !strings.Contains(text, "\x1b[K") {
		return nil
	}
	if strings.Contains(text, "Rate limiting is in effect") {
		return nil
	}
	botPrefix := "[" + botNick + "] \x1b"
	if !strings.HasPrefix(text, botPrefix) {
		return nil
	}
	if actionStart := strings.Index(text, "\x1b[K** "); actionStart != -1 {
		actionPart := strings.TrimSuffix(text[actionStart+6:], "\r")
		parts := strings.SplitN(actionPart, " ", 2)
		if len(parts) < 2 {
			return nil
		}
		return &config.Message{
			Username: parts[0],
			Text:     parts[1],
			Channel:  "sshchat",
			UserID:   "nick",
			Event:    config.EventUserAction,
		}
	}
	kIndex := strings.Index(text, "\x1b[K")
	if kIndex == -1 {
		return nil
	}
	messagePart := strings.TrimSuffix(text[kIndex+3:], "\r")
	// `-> ...` is SystemMsg (private to bot); ` * ...` is AnnounceMsg
	// (join/leave/server notice). Neither is real chat.
	if strings.HasPrefix(messagePart, "-> ") || strings.HasPrefix(messagePart, " * ") {
		return nil
	}
	colonIndex := strings.Index(messagePart, ": ")
	if colonIndex <= 0 {
		return nil
	}
	username := messagePart[:colonIndex]
	body := messagePart[colonIndex+2:]
	if username == "system" || username == botNick {
		return nil
	}
	return &config.Message{
		Username: username,
		Text:     body,
		Channel:  "sshchat",
		UserID:   "nick",
	}
}

func (b *Bsshchat) handleSSHChat() error {
	/*
		done := b.sshchatKeepAlive()
		defer close(done)
	*/
	wait := true
	for {
		if b.r.Scan() {
			text := b.r.Text()
			if b.GetBool("Debug") {
				b.Log.Debugf("Raw SSH chat line: %q", text)
			}
			if !strings.Contains(text, "\033[K") {
				continue
			}
			if strings.Contains(text, "Rate limiting is in effect") {
				continue
			}
			botNick := b.GetString("Nick")
			if msg := sshChatParse(text, botNick); msg != nil {
				msg.Account = b.Account
				b.Remote <- *msg
				continue
			}
			botPrefix := "[" + botNick + "] \x1b"
			if strings.HasPrefix(text, botPrefix) {
				continue
			}
			res := strings.Split(stripPrompt(text), ":")
			if res[0] == "-> Set theme" {
				wait = false
				if b.GetBool("Debug") {
					b.Log.Debugf("mono found, allowing")
				}
				continue
			}
			if !wait {
				if b.GetBool("Debug") {
					b.Log.Debugf("<= Message %#v", res)
				}

				// Regular message parsing (legacy fallback)
				messageText := strings.TrimSpace(strings.Join(res[1:], ":"))

				// Create the message with default values
				rmsg := config.Message{Username: res[0], Text: messageText, Channel: "sshchat", Account: b.Account, UserID: "nick"}

				b.Remote <- rmsg
			}
		}
	}
}

func (b *Bsshchat) handleUploadFile(msg *config.Message) (string, error) {
	for _, f := range msg.Extra["file"] {
		fi := f.(config.FileInfo)
		if fi.Comment != "" {
			msg.Text += fi.Comment + ": "
		}
		if fi.URL != "" {
			msg.Text = fi.URL
			if fi.Comment != "" {
				msg.Text = fi.Comment + ": " + fi.URL
			}
		}
		if _, err := b.w.Write([]byte(msg.Username + msg.Text + "\r\n")); err != nil {
			b.Log.Errorf("Could not send file message: %#v", err)
		}
	}
	return "", nil
}

// extractOriginalNick attempts to extract the original nickname from RemoteNickFormat
func (b *Bsshchat) extractOriginalNick(formattedUsername string) string {
	// Try to extract nick from common RemoteNickFormat patterns like "<nick>", "[PROTOCOL] <nick>", etc.
	// First try angle brackets: <nick>
	re := regexp.MustCompile(`<([^>]+)>`)
	matches := re.FindStringSubmatch(formattedUsername)
	if len(matches) > 1 {
		return matches[1]
	}

	// If no pattern matches, return the formatted username as-is
	return formattedUsername
}
