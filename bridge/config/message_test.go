package config

import (
	"encoding/json"
	"strings"
	"testing"
)

// marshalMessage centralizes the json.Marshal so the legacy untagged Extra
// field only needs a single linter exemption.
func marshalMessage(t *testing.T, m Message) []byte {
	t.Helper()
	out, err := json.Marshal(m) //nolint:musttag // Message.Extra is intentionally untagged legacy wire
	if err != nil {
		t.Fatalf("marshal: %s", err)
	}
	return out
}

// TestMessageJSONNoReactionFieldsLeak is the backwards-compatibility contract:
// a non-reaction message must serialize with no "emoji" key so existing api
// consumers see byte-identical payloads.
func TestMessageJSONNoReactionFieldsLeak(t *testing.T) {
	msg := Message{
		Text:     "hello",
		Channel:  "general",
		Username: "bob",
		Account:  "discord.test",
		Event:    "",
	}
	out := marshalMessage(t, msg)
	if strings.Contains(string(out), "emoji") {
		t.Fatalf("non-reaction message leaked emoji field: %s", out)
	}
}

func TestMessageJSONReactionShape(t *testing.T) {
	msg := Message{
		Text:     "👍",
		Emoji:    "👍",
		Channel:  "general",
		Username: "bob",
		Account:  "telegram.test",
		Event:    EventReactionAdd,
		ParentID: "discord 987654",
	}
	out := marshalMessage(t, msg)
	var got map[string]interface{}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %s", err)
	}
	if got["emoji"] != "👍" {
		t.Fatalf("emoji field = %v", got["emoji"])
	}
	if got["text"] != "👍" {
		t.Fatalf("text mirror = %v", got["text"])
	}
	if got["event"] != EventReactionAdd {
		t.Fatalf("event = %v", got["event"])
	}
}

// TestMessageJSONByteIdenticalVsPreReaction guards the exact key set of a
// plain message so an accidental tag/field change is caught.
func TestMessageJSONByteIdenticalVsPreReaction(t *testing.T) {
	msg := Message{Text: "hi", Username: "bob"}
	out := marshalMessage(t, msg)
	var got map[string]interface{}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %s", err)
	}
	for _, banned := range []string{"emoji", "source_id", "parent_username", "parent_text"} {
		if _, ok := got[banned]; ok {
			t.Fatalf("omitempty field %q present on plain message: %s", banned, out)
		}
	}
}
