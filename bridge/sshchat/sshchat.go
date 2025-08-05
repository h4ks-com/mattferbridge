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

func (b *Bsshchat) handleSSHChat() error {
	/*
		done := b.sshchatKeepAlive()
		defer close(done)
	*/
	wait := true
	for {
		if b.r.Scan() {
			if b.GetBool("Debug") {
				b.Log.Debugf("Raw SSH chat line: %q", b.r.Text())
			}
			// ignore messages from ourselves
			if !strings.Contains(b.r.Text(), "\033[K") {
				if b.GetBool("Debug") {
					b.Log.Debugf("Skipping line without \\033[K")
				}
				continue
			}
			if strings.Contains(b.r.Text(), "Rate limiting is in effect") {
				continue
			}

			// skip our own messages
			botPrefix := "[" + b.GetString("Nick") + "] \x1b"
			if strings.HasPrefix(b.r.Text(), botPrefix) {

				// Check if this is an action message: "\x1b[K** username action"
				if strings.Contains(b.r.Text(), "\x1b[K** ") {
					actionStart := strings.Index(b.r.Text(), "\x1b[K** ")
					if actionStart != -1 {
						actionPart := b.r.Text()[actionStart+6:] // Skip "\x1b[K** "
						actionPart = strings.TrimSuffix(actionPart, "\r")
						parts := strings.SplitN(actionPart, " ", 2)
						if len(parts) >= 2 {
							username := parts[0]
							actionText := parts[1]
							rmsg := config.Message{
								Username: username,
								Text:     actionText,
								Channel:  "sshchat",
								Account:  b.Account,
								UserID:   "nick",
								Event:    config.EventUserAction,
							}
							if b.GetBool("Debug") {
								b.Log.Debugf("Detected SSH action from %s: %s", username, actionText)
							}
							b.Remote <- rmsg
							continue
						}
					}
				}

				// Check if this is a regular user message: "\x1b[...D\x1b[Kusername: message"
				if strings.Contains(b.r.Text(), "\x1b[K") {
					kIndex := strings.Index(b.r.Text(), "\x1b[K")
					if kIndex != -1 {
						messagePart := b.r.Text()[kIndex+3:] // Skip "\x1b[K"
						messagePart = strings.TrimSuffix(messagePart, "\r")

						// Parse "username: message" format
						if strings.Contains(messagePart, ": ") {
							colonIndex := strings.Index(messagePart, ": ")
							username := messagePart[:colonIndex]
							messageText := messagePart[colonIndex+2:]

							// Skip system messages and our own messages
							if username != "system" && username != b.GetString("Nick") {
								rmsg := config.Message{
									Username: username,
									Text:     messageText,
									Channel:  "sshchat",
									Account:  b.Account,
									UserID:   "nick",
								}
								b.Remote <- rmsg
								continue
							} else {
							}
						}
					}
				}

				// Skip all other messages from our bot
				continue
			}
			res := strings.Split(stripPrompt(b.r.Text()), ":")
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
