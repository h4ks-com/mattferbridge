package birc

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/42wim/matterbridge/bridge/config"
	"github.com/42wim/matterbridge/bridge/helper"
	"github.com/lrstanley/girc"
)

var imageMagicPrefixes = [][]byte{
	{0x89, 'P', 'N', 'G'},
	{0xff, 0xd8, 0xff},
	{'G', 'I', 'F', '8'},
}

const avatarKey = "avatar"

// IRC avatars use the IRCv3 draft/metadata-2 capability: clients SET an
// `avatar` key with a URL, the server pushes that key to subscribers and
// replies to GETs. We rehost the URL on the configured MediaServerUpload so
// downstream bridges (notably Discord webhooks) always see an HTTPS URL of
// known shape, and so the avatar survives the source host going down.

// METADATA push: `:server METADATA <target> <key> <visibility> [<value>]`.
func (b *Birc) handleMetadataPush(client *girc.Client, event girc.Event) {
	if len(event.Params) < 4 {
		return
	}
	target, key, value := event.Params[0], event.Params[1], event.Last()
	if !strings.EqualFold(key, avatarKey) {
		return
	}
	b.processAvatarMetadata(extractNick(target), value)
}

// RPL_KEYVALUE (761) and RPL_METADATAWHOIS (769) prepend our own nick.
func (b *Birc) handleMetadataKeyValue(client *girc.Client, event girc.Event) {
	if len(event.Params) < 5 {
		return
	}
	target, key, value := event.Params[1], event.Params[2], event.Last()
	if !strings.EqualFold(key, avatarKey) {
		return
	}
	b.processAvatarMetadata(extractNick(target), value)
}

// Targets prefixed `#` or `&` are channels; we only care about user avatars.
func extractNick(target string) string {
	if target == "" || target[0] == '#' || target[0] == '&' {
		return ""
	}
	if i := strings.IndexByte(target, '!'); i > 0 {
		return target[:i]
	}
	return target
}

func (b *Birc) processAvatarMetadata(nick, url string) {
	if nick == "" || url == "" {
		return
	}
	// Reject `data:`, `javascript:`, relatives, and the `::https://` artifact
	// produced by clients that double-prefix the IRC trailing-parameter colon.
	if !isAbsoluteHTTPURL(url) {
		b.Log.Debugf("Ignoring non-http avatar URL for %s: %q", nick, url)
		return
	}
	if b.General.MediaServerUpload == "" {
		return
	}

	b.avatarMu.Lock()
	cachedURL, alreadyCached := b.avatarOriginal(nick)
	if alreadyCached && cachedURL == url {
		b.avatarMu.Unlock()
		return
	}
	// Reserve before spawn so the connect-time METADATA SUB burst doesn't fan out.
	b.avatarMap[":url:"+nick] = url
	b.avatarMu.Unlock()

	go b.downloadAndUploadAvatar(nick, url)
}

func isAbsoluteHTTPURL(url string) bool {
	return strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "http://")
}

// Hidden key alongside the sha mapping; lets us detect URL changes and skip
// re-downloading when the user republishes the same URL.
func (b *Birc) avatarOriginal(nick string) (string, bool) {
	url, ok := b.avatarMap[":url:"+nick]
	return url, ok
}

func (b *Birc) downloadAndUploadAvatar(nick, url string) {
	data, err := helper.DownloadFile(url)
	if err != nil {
		b.Log.Debugf("Avatar download failed for %s (%s): %s", nick, url, err)
		return
	}
	// Magic-byte sniff: a user's avatar URL is untrusted content and we don't
	// want to rehost arbitrary payloads on s.h4ks.com.
	if !looksLikeImage(*data) {
		b.Log.Debugf("Avatar at %s isn't an image, skipping", url)
		return
	}

	// girafiles reserves <bucket>/<alias> forever, so reuse 500s even after TTL expiry.
	name := fmt.Sprintf("%s_%d.png", nick, time.Now().Unix())
	// Gateway only routes EventAvatarDownload to destinations whose channel
	// matches the source's; without a Channel the loopback never fires and
	// cacheAvatar wouldn't populate b.avatarMap.
	channel := b.anyJoinedChannel()
	if channel == "" {
		b.Log.Debugf("Skipping avatar upload for %s: no joined channel yet", nick)
		return
	}
	rmsg := config.Message{
		Username: "system",
		Text:     "avatar",
		Channel:  channel,
		Account:  b.Account,
		UserID:   nick,
		Event:    config.EventAvatarDownload,
		Extra:    make(map[string][]interface{}),
	}
	if err := helper.HandleDownloadSize(b.Log, &rmsg, name, int64(len(*data)), b.General); err != nil {
		b.Log.Debugf("Avatar size check failed for %s: %s", nick, err)
		return
	}
	helper.HandleDownloadData(b.Log, &rmsg, name, "", "", data, b.General)

	b.Remote <- rmsg
}

func looksLikeImage(b []byte) bool {
	if len(b) < 12 {
		return false
	}
	for _, magic := range imageMagicPrefixes {
		if bytes.HasPrefix(b, magic) {
			return true
		}
	}
	return bytes.HasPrefix(b, []byte("RIFF")) && bytes.Equal(b[8:12], []byte("WEBP"))
}

// Lazy GET on first sight of a nick so we don't need every user to re-SET
// their avatar to be discovered after we connect.
func (b *Birc) requestAvatarOnce(nick string) {
	if nick == "" || nick == b.Nick {
		return
	}
	if !b.i.HasCapability("draft/metadata-2") {
		return
	}
	b.avatarMu.Lock()
	if b.avatarQueried[nick] {
		b.avatarMu.Unlock()
		return
	}
	b.avatarQueried[nick] = true
	b.avatarMu.Unlock()
	if err := b.i.Cmd.SendRaw(fmt.Sprintf("METADATA %s GET avatar", nick)); err != nil {
		b.Log.Debugf("METADATA GET avatar for %s failed: %s", nick, err)
	}
}

// Store fi.URL not fi.SHA: our upload filename varies per-upload, so the
// <sha>/<nick>.png reconstruction in helper.GetAvatar would 404.
func (b *Birc) cacheAvatar(msg *config.Message) (string, error) {
	if len(msg.Extra["file"]) == 0 {
		return "", nil
	}
	fi, ok := msg.Extra["file"][0].(config.FileInfo)
	if !ok {
		return "", nil
	}
	if fi.URL == "" {
		return "", nil
	}
	b.avatarMu.Lock()
	b.avatarMap[msg.UserID] = fi.URL
	b.avatarMu.Unlock()
	b.Log.Debugf("Cached avatar URL %s for %s", fi.URL, msg.UserID)
	return "", nil
}

func (b *Birc) avatarURLFor(nick string) string {
	b.avatarMu.Lock()
	defer b.avatarMu.Unlock()
	return b.avatarMap[nick]
}

func (b *Birc) anyJoinedChannel() string {
	for ch := range b.channels {
		return ch
	}
	return ""
}
