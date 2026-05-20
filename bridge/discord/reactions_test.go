package bdiscord

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestDiscordReactionEmoji(t *testing.T) {
	cases := []struct {
		name  string
		emoji discordgo.Emoji
		want  string
	}{
		{"unicode", discordgo.Emoji{Name: "👍"}, "👍"},
		{"custom keeps name:id", discordgo.Emoji{Name: "blobwave", ID: "1234567890"}, "blobwave:1234567890"},
		{"empty", discordgo.Emoji{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := discordReactionEmoji(tc.emoji); got != tc.want {
				t.Fatalf("discordReactionEmoji = %q, want %q", got, tc.want)
			}
		})
	}
}
