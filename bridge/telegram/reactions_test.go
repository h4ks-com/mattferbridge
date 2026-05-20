package btelegram

import (
	"sort"
	"strings"
	"testing"

	"github.com/42wim/matterbridge/bridge/config"
	tgbotapi "github.com/matterbridge/telegram-bot-api/v6"
)

func emojiReactions(emojis ...string) []tgbotapi.ReactionType {
	out := make([]tgbotapi.ReactionType, 0, len(emojis))
	for _, e := range emojis {
		out = append(out, tgbotapi.ReactionType{Type: "emoji", Emoji: e})
	}
	return out
}

func TestDiffReactions(t *testing.T) {
	cases := []struct {
		name         string
		old, updated []tgbotapi.ReactionType
		wantAdd      []string
		wantRemove   []string
	}{
		{"add to empty", nil, emojiReactions("👍"), []string{"👍"}, nil},
		{"remove to empty", emojiReactions("👍"), nil, nil, []string{"👍"}},
		{"swap", emojiReactions("👍"), emojiReactions("❤️"), []string{"❤️"}, []string{"👍"}},
		{"no change", emojiReactions("🔥"), emojiReactions("🔥"), nil, nil},
		{"custom emoji ignored", nil, []tgbotapi.ReactionType{{Type: "custom_emoji", CustomEmojiID: "123"}}, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			add, remove := diffReactions(tc.old, tc.updated)
			sort.Strings(add)
			sort.Strings(remove)
			if !equalStrings(add, tc.wantAdd) {
				t.Fatalf("added = %v, want %v", add, tc.wantAdd)
			}
			if !equalStrings(remove, tc.wantRemove) {
				t.Fatalf("removed = %v, want %v", remove, tc.wantRemove)
			}
		})
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestReactionUsername(t *testing.T) {
	if got := reactionUsername(&tgbotapi.User{UserName: "bob"}); got != "bob" {
		t.Fatalf("username = %q", got)
	}
	if got := reactionUsername(&tgbotapi.User{FirstName: "Bob", LastName: "Jones"}); got != "Bob Jones" {
		t.Fatalf("name = %q", got)
	}
}

func TestSetMessageReactionParamsAdd(t *testing.T) {
	params, err := setMessageReactionParams(-100123, 42, config.EventReactionAdd, "👍")
	if err != nil {
		t.Fatalf("build params: %s", err)
	}
	if params["chat_id"] != "-100123" {
		t.Fatalf("chat_id = %q", params["chat_id"])
	}
	if params["message_id"] != "42" {
		t.Fatalf("message_id = %q", params["message_id"])
	}
	r := params["reaction"]
	if !strings.Contains(r, `"type":"emoji"`) || !strings.Contains(r, `"emoji":"👍"`) {
		t.Fatalf("reaction json = %q", r)
	}
}

func TestSetMessageReactionParamsRemove(t *testing.T) {
	params, err := setMessageReactionParams(-100123, 42, config.EventReactionRemove, "")
	if err != nil {
		t.Fatalf("build params: %s", err)
	}
	// Absent reaction array clears the bot's reaction.
	if _, ok := params["reaction"]; ok {
		t.Fatalf("remove must not set reaction param, got %q", params["reaction"])
	}
	if params["message_id"] != "42" {
		t.Fatalf("message_id = %q", params["message_id"])
	}
}
