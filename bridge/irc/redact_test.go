package birc

import (
	"testing"

	"github.com/42wim/matterbridge/bridge/config"
	"github.com/lrstanley/girc"
)

func TestHandleRedact(t *testing.T) {
	b, remote := newReactionTestBirc(t)
	b.Nick = "bot"

	b.handleRedact(nil, *girc.ParseEvent(`:bob!user@host REDACT #H4ks abc123 :oops`))
	select {
	case msg := <-remote:
		if msg.Event != config.EventMsgDelete || msg.ID != "abc123" || msg.Channel != "#h4ks" {
			t.Fatalf("got %+v", msg)
		}
	default:
		t.Fatal("no delete emitted")
	}

	for _, line := range []string{
		`:bot!user@host REDACT #h4ks abc123`,
		`:bob!user@host REDACT bot abc123`,
		`:bob!user@host REDACT #h4ks`,
	} {
		b.handleRedact(nil, *girc.ParseEvent(line))
		select {
		case msg := <-remote:
			t.Fatalf("%q must not emit, got %+v", line, msg)
		default:
		}
	}
}

func TestQueueRedaction(t *testing.T) {
	b, _ := newReactionTestBirc(t)
	b.MessageQueue = 1
	b.Local = make(chan localMsg, 2)

	b.queueRedaction(config.Message{Event: config.EventMsgDelete, Channel: "#h4ks"})
	if len(b.Local) != 0 {
		t.Fatal("redaction without an id must not be queued")
	}

	b.queueRedaction(config.Message{Event: config.EventMsgDelete, Channel: "#h4ks", ID: "abc123"})
	b.queueRedaction(config.Message{Event: config.EventMsgDelete, Channel: "#h4ks", ID: "def456"})
	if len(b.Local) != 1 {
		t.Fatalf("queue len = %d, want 1 (second dropped at MessageQueue)", len(b.Local))
	}
	if lm := <-b.Local; lm.msg.ID != "abc123" {
		t.Fatalf("queued id = %q", lm.msg.ID)
	}
}
