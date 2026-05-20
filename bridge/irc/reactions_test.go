package birc

import (
	"io"
	"testing"

	"github.com/42wim/matterbridge/bridge"
	"github.com/42wim/matterbridge/bridge/config"
	"github.com/lrstanley/girc"
	"github.com/sirupsen/logrus"
)

func TestFormatReactionTagMsg(t *testing.T) {
	cases := []struct {
		name    string
		event   string
		emoji   string
		parent  string
		channel string
		want    string
	}{
		{
			name: "react", event: config.EventReactionAdd, emoji: "👍", parent: "abc123", channel: "#h4ks",
			want: "@+draft/react=👍;+reply=abc123;+draft/reply=abc123 TAGMSG #h4ks",
		},
		{
			name: "unreact", event: config.EventReactionRemove, emoji: "🇦🇷", parent: "xyz", channel: "#bots",
			want: "@+draft/unreact=🇦🇷;+reply=xyz;+draft/reply=xyz TAGMSG #bots",
		},
		{
			name: "escapes tag metachars in value", event: config.EventReactionAdd, emoji: "a;b c", parent: "p;q", channel: "#bots",
			want: "@+draft/react=a\\:b\\sc;+reply=p\\:q;+draft/reply=p\\:q TAGMSG #bots",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatReactionTagMsg(tc.event, tc.emoji, tc.parent, tc.channel)
			if got != tc.want {
				t.Fatalf("formatReactionTagMsg = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReactionParentID(t *testing.T) {
	// +reply preferred
	e := girc.ParseEvent(`@+reply=123;+draft/react=👋 :bob!u@h TAGMSG #c`)
	if got := reactionParentID(*e); got != "123" {
		t.Fatalf("+reply parent = %q", got)
	}
	// +draft/reply fallback
	e = girc.ParseEvent(`@+draft/reply=456;+draft/react=👋 :bob!u@h TAGMSG #c`)
	if got := reactionParentID(*e); got != "456" {
		t.Fatalf("+draft/reply parent = %q", got)
	}
	// no parent
	e = girc.ParseEvent(`@+draft/react=👋 :bob!u@h TAGMSG #c`)
	if got := reactionParentID(*e); got != "" {
		t.Fatalf("no-parent should be empty, got %q", got)
	}
}

func newReactionTestBirc(t *testing.T) (*Birc, chan config.Message) {
	t.Helper()
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	remote := make(chan config.Message, 1)
	b := &Birc{
		i:             girc.New(girc.Config{Server: "localhost", Port: 6667, Nick: "bot", User: "bot"}),
		avatarMap:     make(map[string]string),
		avatarQueried: make(map[string]bool),
	}
	b.Config = &bridge.Config{
		Bridge: &bridge.Bridge{Account: "irc.test", Log: logrus.NewEntry(logger)},
		Remote: remote,
	}
	return b, remote
}

func TestEmitReaction(t *testing.T) {
	b, remote := newReactionTestBirc(t)

	e := girc.ParseEvent(`@+reply=abc;+draft/react=👍 :bob!user@host TAGMSG #h4ks`)
	emoji, _ := e.Tags.Get("+draft/react")
	b.emitReaction(*e, "#h4ks", "abc", emoji, config.EventReactionAdd)

	select {
	case msg := <-remote:
		if msg.Event != config.EventReactionAdd {
			t.Fatalf("event = %q", msg.Event)
		}
		if msg.Emoji != "👍" || msg.Text != "👍" {
			t.Fatalf("emoji/text = %q/%q", msg.Emoji, msg.Text)
		}
		if msg.ParentID != "abc" {
			t.Fatalf("parent = %q", msg.ParentID)
		}
		if msg.Username != "bob" || msg.UserID != "user@host" {
			t.Fatalf("username/userid = %q/%q", msg.Username, msg.UserID)
		}
		if msg.Channel != "#h4ks" {
			t.Fatalf("channel = %q", msg.Channel)
		}
	default:
		t.Fatal("no reaction emitted")
	}
}

func TestEmitReactionEmptyEmojiDropped(t *testing.T) {
	b, remote := newReactionTestBirc(t)
	e := girc.ParseEvent(`@+reply=abc :bob!user@host TAGMSG #h4ks`)
	b.emitReaction(*e, "#h4ks", "abc", "", config.EventReactionAdd)
	select {
	case <-remote:
		t.Fatal("empty emoji must not emit")
	default:
	}
}
