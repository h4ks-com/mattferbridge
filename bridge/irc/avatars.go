package birc

import (
	"bytes"
	"fmt"
	"strings"

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

// handleMetadataPush handles unsolicited METADATA events the server pushes to
// us because we subscribed (see handleMetadataSubscribe).
//
// Notification shape: `:server METADATA <Target> <Key> <Visibility> [<Value>]`.
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

// handleMetadataKeyValue handles RPL_KEYVALUE (761) and RPL_METADATAWHOIS (769)
// which carry an extra leading parameter (our own nick).
//
// Reply shape: `<client> <target> <key> <visibility> [<value>]`.
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

// extractNick pulls just the nick out of either `nick` or `nick!user@host`.
func extractNick(target string) string {
	if i := strings.IndexByte(target, '!'); i > 0 {
		return target[:i]
	}
	return target
}

// processAvatarMetadata validates the URL and kicks off an async re-host via
// MediaServerUpload (mirroring telegram's avatar flow). nick is the bare nick.
func (b *Birc) processAvatarMetadata(nick, url string) {
	if nick == "" || url == "" {
		return
	}
	if !isAbsoluteHTTPURL(url) {
		b.Log.Debugf("Ignoring non-http avatar URL for %s: %q", nick, url)
		return
	}
	if b.General.MediaServerUpload == "" {
		// No place to rehost; without it Discord can't fetch the avatar reliably.
		return
	}

	b.avatarMu.Lock()
	cachedURL, alreadyCached := b.avatarOriginal(nick)
	if alreadyCached && cachedURL == url {
		b.avatarMu.Unlock()
		return
	}
	b.avatarMu.Unlock()

	go b.downloadAndUploadAvatar(nick, url)
}

// isAbsoluteHTTPURL guards against the IRCv3 wire-encoding artifact `::https://`
// (double-colon) and other shapes (`data:`, `javascript:`, relative paths).
func isAbsoluteHTTPURL(url string) bool {
	return strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "http://")
}

// avatarOriginal returns the source URL we used to populate the cache, so we
// can detect changes and avoid redundant downloads. We store it as a hidden
// "<nick>:url" entry alongside the sha mapping.
func (b *Birc) avatarOriginal(nick string) (string, bool) {
	url, ok := b.avatarMap[":url:"+nick]
	return url, ok
}

// downloadAndUploadAvatar fetches the avatar from the user-supplied URL, runs
// it through matterbridge's standard download helper, then emits an
// EventAvatarDownload to the gateway which re-hosts it on MediaServerUpload.
// The result loops back to Send() → cacheAvatar() to populate b.avatarMap.
func (b *Birc) downloadAndUploadAvatar(nick, url string) {
	data, err := helper.DownloadFile(url)
	if err != nil {
		b.Log.Debugf("Avatar download failed for %s (%s): %s", nick, url, err)
		return
	}
	if !looksLikeImage(*data) {
		b.Log.Debugf("Avatar at %s isn't an image, skipping", url)
		return
	}

	name := nick + ".png"
	rmsg := config.Message{
		Username: "system",
		Text:     "avatar",
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

	b.avatarMu.Lock()
	b.avatarMap[":url:"+nick] = url
	b.avatarMu.Unlock()

	b.Remote <- rmsg
}

// looksLikeImage sniffs the leading bytes for PNG/JPEG/GIF/WebP magic numbers.
// Avoids re-hosting arbitrary content a user pointed their avatar URL at.
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

// requestAvatarOnce sends `METADATA <nick> GET avatar` the first time we see
// a user, so we learn their avatar without waiting for them to SET it again.
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

// cacheAvatar stores the sha emitted by gateway after re-hosting completes.
// Mirrors Btelegram.cacheAvatar so helper.GetAvatar resolves to the rehosted
// URL on subsequent sends.
func (b *Birc) cacheAvatar(msg *config.Message) (string, error) {
	if len(msg.Extra["file"]) == 0 {
		return "", nil
	}
	fi, ok := msg.Extra["file"][0].(config.FileInfo)
	if !ok {
		return "", nil
	}
	if fi.SHA == "" {
		return "", nil
	}
	b.avatarMu.Lock()
	b.avatarMap[msg.UserID] = fi.SHA
	b.avatarMu.Unlock()
	b.Log.Debugf("Cached avatar %s for %s", fi.SHA, msg.UserID)
	return "", nil
}

// avatarURLFor returns the public MediaServerDownload URL for a nick's avatar,
// or "" if we haven't (yet) seen one.
func (b *Birc) avatarURLFor(nick string) string {
	b.avatarMu.Lock()
	defer b.avatarMu.Unlock()
	return helper.GetAvatar(b.avatarMap, nick, b.General)
}
