package bsshchat

import (
	"testing"

	"github.com/42wim/matterbridge/bridge/config"
)

func TestSSHChatParse(t *testing.T) { //nolint:funlen
	const botNick = "_"
	// prompt + cursor-move; the exact <n> in `\x1b[<n>D` is irrelevant to the parser.
	pref := "[" + botNick + "] \x1b[9D"

	tests := []struct {
		name string
		raw  string
		want *config.Message
	}{
		{
			name: "SystemMsg theme reply is dropped",
			raw:  pref + "\x1b[K-> Set theme: mono\r",
			want: nil,
		},
		{
			name: "SystemMsg quiet reply is dropped",
			raw:  pref + "\x1b[K-> Quiet mode is toggled ON\r",
			want: nil,
		},
		{
			name: "AnnounceMsg join is dropped",
			raw:  pref + "\x1b[K * mattf joined. (Connected: 1)\r",
			want: nil,
		},
		{
			name: "AnnounceMsg leave is dropped",
			raw:  pref + "\x1b[K * mattf left.\r",
			want: nil,
		},
		{
			name: "Regular user message relays",
			raw:  pref + "\x1b[Kalice: hello world\r",
			want: &config.Message{Username: "alice", Text: "hello world", Channel: "sshchat", UserID: "nick"},
		},
		{
			name: "User message containing arrow body relays",
			raw:  pref + "\x1b[Kalice: -> arrow content\r",
			want: &config.Message{Username: "alice", Text: "-> arrow content", Channel: "sshchat", UserID: "nick"},
		},
		{
			name: "User message containing colon body relays",
			raw:  pref + "\x1b[Kalice: 10:30 meeting\r",
			want: &config.Message{Username: "alice", Text: "10:30 meeting", Channel: "sshchat", UserID: "nick"},
		},
		{
			name: "Action message relays as user_action",
			raw:  pref + "\x1b[K** alice waves at everyone\r",
			want: &config.Message{Username: "alice", Text: "waves at everyone", Channel: "sshchat", UserID: "nick", Event: config.EventUserAction},
		},
		{
			name: "Bot's own echo is dropped",
			raw:  pref + "\x1b[K" + botNick + ": echo back\r",
			want: nil,
		},
		{
			name: "system user is dropped",
			raw:  pref + "\x1b[Ksystem: server announcement\r",
			want: nil,
		},
		{
			name: "Rate limit line is dropped",
			raw:  pref + "\x1b[KRate limiting is in effect for you. Slow down\r",
			want: nil,
		},
		{
			name: "Line without escape codes is dropped",
			raw:  "plain text line\r",
			want: nil,
		},
		{
			name: "Non-bot-prefix line is not handled here",
			raw:  "\x1b[38;05;245m * someone joined.\x1b[0m\r",
			want: nil,
		},
		{
			name: "AnnounceMsg with elapsed time is dropped",
			raw:  pref + "\x1b[K * mattf left. (After 15.6 hours)\r",
			want: nil,
		},
		{
			name: "User message starting with asterisk relays",
			raw:  pref + "\x1b[Kalice: * not an announcement\r",
			want: &config.Message{Username: "alice", Text: "* not an announcement", Channel: "sshchat", UserID: "nick"},
		},
		{
			name: "User message with empty body relays empty text",
			raw:  pref + "\x1b[Kalice: \r",
			want: &config.Message{Username: "alice", Text: "", Channel: "sshchat", UserID: "nick"},
		},
		{
			name: "Action with single-word body has no body, dropped",
			raw:  pref + "\x1b[K** alice\r",
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sshChatParse(tc.raw, botNick)
			if (got == nil) != (tc.want == nil) {
				t.Fatalf("relay-decision mismatch: got=%+v want=%+v", got, tc.want)
			}
			if got == nil {
				return
			}
			if got.Username != tc.want.Username {
				t.Errorf("Username: got=%q want=%q", got.Username, tc.want.Username)
			}
			if got.Text != tc.want.Text {
				t.Errorf("Text: got=%q want=%q", got.Text, tc.want.Text)
			}
			if got.Event != tc.want.Event {
				t.Errorf("Event: got=%q want=%q", got.Event, tc.want.Event)
			}
			if got.Channel != tc.want.Channel {
				t.Errorf("Channel: got=%q want=%q", got.Channel, tc.want.Channel)
			}
			if got.UserID != tc.want.UserID {
				t.Errorf("UserID: got=%q want=%q", got.UserID, tc.want.UserID)
			}
		})
	}
}
