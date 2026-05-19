package birc

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"strconv"
	"strings"
	"time"

	"github.com/42wim/matterbridge/bridge/config"
	"github.com/42wim/matterbridge/bridge/helper"
	"github.com/lrstanley/girc"
	"github.com/paulrosania/go-charset/charset"
	"github.com/saintfish/chardet"

	// We need to import the 'data' package as an implicit dependency.
	// See: https://godoc.org/github.com/paulrosania/go-charset/charset
	_ "github.com/paulrosania/go-charset/data"
)

func (b *Birc) handleCharset(msg *config.Message) error {
	if b.GetString("Charset") != "" {
		switch b.GetString("Charset") {
		case "gbk", "gb18030", "gb2312", "big5", "euc-kr", "euc-jp", "shift-jis", "iso-2022-jp":
			msg.Text = toUTF8(b.GetString("Charset"), msg.Text)
		default:
			buf := new(bytes.Buffer)
			w, err := charset.NewWriter(b.GetString("Charset"), buf)
			if err != nil {
				b.Log.Errorf("charset to utf-8 conversion failed: %s", err)
				return err
			}
			fmt.Fprint(w, msg.Text)
			w.Close()
			msg.Text = buf.String()
		}
	}
	return nil
}

// handleFiles returns true if we have handled the files, otherwise return false
func (b *Birc) handleFiles(msg *config.Message) bool {
	if msg.Extra == nil {
		return false
	}
	for _, rmsg := range helper.HandleExtra(msg, b.General) {
		b.Local <- localMsg{msg: rmsg, resultCh: make(chan string, 1)}
	}
	if len(msg.Extra["file"]) == 0 {
		return false
	}
	for _, f := range msg.Extra["file"] {
		fi := f.(config.FileInfo)
		if fi.Comment != "" {
			msg.Text += fi.Comment + " : "
		}
		if fi.URL != "" {
			msg.Text = fi.URL
			if fi.Comment != "" {
				msg.Text = fi.Comment + " : " + fi.URL
			}
		}
		b.Local <- localMsg{msg: config.Message{Text: msg.Text, Username: msg.Username, Channel: msg.Channel, Event: msg.Event}, resultCh: make(chan string, 1)}
	}
	return true
}

func (b *Birc) handleInvite(client *girc.Client, event girc.Event) {
	if len(event.Params) != 2 {
		return
	}

	channel := event.Params[1]

	b.Log.Debugf("got invite for %s", channel)

	if _, ok := b.channels[channel]; ok {
		b.i.Cmd.Join(channel)
	}
}

func (b *Birc) handleQuit(username string, event girc.Event) {
	if username == b.Nick && strings.Contains(event.Last(), "Ping timeout") {
		b.Log.Infof("%s reconnecting ..", b.Account)
		b.Remote <- config.Message{Username: "system", Text: "reconnect", Channel: "", Account: b.Account, Event: config.EventFailure}
		return
	}
	if username == b.Nick || b.GetBool("nosendjoinpart") {
		return
	}
	for channel, users := range b.channelUsers {
		if _, userInChannel := users[username]; !userInChannel {
			continue
		}
		delete(users, username)
		msg := config.Message{
			Username: "system",
			Text:     username + " quits",
			Channel:  channel,
			Account:  b.Account,
			Event:    config.EventJoinLeave,
		}
		if b.GetBool("verbosejoinpart") {
			msg.Text = username + " (" + event.Source.Ident + "@" + event.Source.Host + ") quits"
			b.Log.Debugf("<= Sending verbose QUIT event for %s from %s to gateway", username, b.Account)
		} else {
			b.Log.Debugf("<= Sending QUIT event for %s from %s to gateway", username, b.Account)
		}
		b.Log.Debugf("<= Message is %#v", msg)
		b.Remote <- msg
	}
}

func (b *Birc) handleJoinPart(client *girc.Client, event girc.Event) {
	username := event.Source.Name

	if event.Command == "QUIT" {
		b.handleQuit(username, event)
		return
	}

	// Handle regular JOIN/PART/KICK events that have a specific channel
	if len(event.Params) == 0 {
		b.Log.Debugf("handleJoinPart: empty Params? %#v", event)
		return
	}
	channel := strings.ToLower(event.Params[0])

	if event.Command == "KICK" && event.Params[1] == b.Nick {
		b.Log.Infof("Got kicked from %s by %s", channel, event.Source.Name)
		time.Sleep(time.Duration(b.GetInt("RejoinDelay")) * time.Second)
		b.Remote <- config.Message{Username: "system", Text: "rejoin", Channel: channel, Account: b.Account, Event: config.EventRejoinChannels}
		return
	}

	// Track user channel membership
	b.trackUserInChannel(username, channel, event.Command)

	if username != b.Nick {
		if b.GetBool("nosendjoinpart") {
			return
		}
		msg := config.Message{Username: "system", Text: username + " " + strings.ToLower(event.Command) + "s", Channel: channel, Account: b.Account, Event: config.EventJoinLeave}
		if b.GetBool("verbosejoinpart") {
			b.Log.Debugf("<= Sending verbose JOIN_LEAVE event from %s to gateway", b.Account)
			msg = config.Message{Username: "system", Text: username + " (" + event.Source.Ident + "@" + event.Source.Host + ") " + strings.ToLower(event.Command) + "s", Channel: channel, Account: b.Account, Event: config.EventJoinLeave}
		} else {
			b.Log.Debugf("<= Sending JOIN_LEAVE event from %s to gateway", b.Account)
		}
		b.Log.Debugf("<= Message is %#v", msg)
		b.Remote <- msg
		return
	}
	b.Log.Debugf("handle %#v", event)
}

func (b *Birc) handleNewConnection(client *girc.Client, event girc.Event) {
	b.Log.Debug("Registering callbacks")
	i := b.i
	b.Nick = event.Params[0]

	b.Log.Debug("Clearing handlers before adding in case of BNC reconnect")
	i.Handlers.Clear("PRIVMSG")
	i.Handlers.Clear("CTCP_ACTION")
	i.Handlers.Clear(girc.RPL_TOPICWHOTIME)
	i.Handlers.Clear(girc.NOTICE)
	i.Handlers.Clear("JOIN")
	i.Handlers.Clear("PART")
	i.Handlers.Clear("QUIT")
	i.Handlers.Clear("KICK")
	i.Handlers.Clear("INVITE")

	i.Handlers.Clear("BATCH")
	i.Handlers.AddBg("PRIVMSG", b.handlePrivMsg)
	i.Handlers.AddBg("BATCH", b.handleBatch)
	i.Handlers.Add(girc.RPL_TOPICWHOTIME, b.handleTopicWhoTime)
	i.Handlers.AddBg(girc.NOTICE, b.handleNotice)
	i.Handlers.AddBg("JOIN", b.handleJoinPart)
	i.Handlers.AddBg("PART", b.handleJoinPart)
	i.Handlers.AddBg("QUIT", b.handleJoinPart)
	i.Handlers.AddBg("KICK", b.handleJoinPart)
	i.Handlers.Add("INVITE", b.handleInvite)
}

func (b *Birc) handleNickServ() {
	if !b.GetBool("UseSASL") && b.GetString("NickServNick") != "" && b.GetString("NickServPassword") != "" {
		b.Log.Debugf("Sending identify to nickserv %s", b.GetString("NickServNick"))
		b.i.Cmd.Message(b.GetString("NickServNick"), "IDENTIFY "+b.GetString("NickServPassword"))
	}
	if strings.EqualFold(b.GetString("NickServNick"), "Q@CServe.quakenet.org") {
		b.Log.Debugf("Authenticating %s against %s", b.GetString("NickServUsername"), b.GetString("NickServNick"))
		b.i.Cmd.Message(b.GetString("NickServNick"), "AUTH "+b.GetString("NickServUsername")+" "+b.GetString("NickServPassword"))
	}
	// give nickserv some slack
	time.Sleep(time.Second * 5)
	b.authDone = true
}

func (b *Birc) handleNotice(client *girc.Client, event girc.Event) {
	if strings.Contains(event.String(), "This nickname is registered") && event.Source.Name == b.GetString("NickServNick") {
		b.handleNickServ()
	} else {
		b.handlePrivMsg(client, event)
	}
}

func (b *Birc) isOurMessageEcho(event girc.Event) bool {
	if event.Command != "PRIVMSG" && event.Command != girc.NOTICE {
		return false
	}
	if event.Echo {
		return true
	}
	if relayer, ok := event.Tags.Get("draft/relaymsg"); ok && relayer == b.Nick {
		return true
	}
	if relayer, ok := event.Tags.Get("relaymsg"); ok && relayer == b.Nick {
		return true
	}
	return false
}

func (b *Birc) handleOther(client *girc.Client, event girc.Event) {
	if b.isOurMessageEcho(event) {
		if msgid, ok := event.Tags.Get("msgid"); ok {
			select {
			case b.echoMsgid <- msgid:
			default:
			}
		}
	}

	debugLevel := b.GetInt("DebugLevel")
	if debugLevel == 0 {
		return
	}
	if debugLevel == 1 {
		if event.Command != "CLIENT_STATE_UPDATED" &&
			event.Command != "CLIENT_GENERAL_UPDATED" {
			b.Log.Debugf("%#v", event.String())
		}
		return
	}
	switch event.Command {
	case "372", "375", "376", "250", "251", "252", "253", "254", "255", "265", "266", "002", "003", "004", "005":
		return
	}
	b.Log.Debugf("%#v", event.String())
}

func (b *Birc) handleOtherAuth(client *girc.Client, event girc.Event) {
	b.handleNickServ()
	b.handleRunCommands()
	b.handleMetadataSubscribe()
	// we are now fully connected
	// only send on first connection
	if b.FirstConnection {
		b.connected <- nil
	}
}

// handleMetadataSubscribe subscribes to the IRCv3 draft/metadata-2 "avatar" key
// so the server pushes live updates when users change their avatar. Silently
// no-ops if the server doesn't advertise the capability.
func (b *Birc) handleMetadataSubscribe() {
	if !b.i.HasCapability("draft/metadata-2") {
		return
	}
	if err := b.i.Cmd.SendRaw("METADATA * SUB avatar"); err != nil {
		b.Log.Debugf("METADATA SUB avatar failed: %s", err)
	}
}

func (b *Birc) handlePrivMsg(client *girc.Client, event girc.Event) {
	if b.skipPrivMsg(event) {
		return
	}

	// IRCv3 draft/multiline: PRIVMSGs tagged with @batch=<ref> for a
	// pending multiline batch are accumulated and flushed as one message
	// when BATCH end arrives (or the safety timeout fires).
	if refTag, ok := event.Tags.Get("batch"); ok {
		if b.appendMultilinePart(refTag, event) {
			return
		}
	}

	rmsg := config.Message{
		Username: event.Source.Name,
		Channel:  strings.ToLower(event.Params[0]),
		Account:  b.Account,
		UserID:   event.Source.Ident + "@" + event.Source.Host,
		Avatar:   b.avatarURLFor(event.Source.Name),
	}
	// Lazy fetch: first time we see a nick, ask the server for their avatar.
	b.requestAvatarOnce(event.Source.Name)

	b.Log.Debugf("== Receiving PRIVMSG: %s %s %#v", event.Source.Name, event.Last(), event)

	if b.GetBool("PreserveThreading") {
		if msgid, ok := event.Tags.Get("msgid"); ok {
			rmsg.ID = msgid
		}

		if replyTo, ok := event.Tags.Get("+reply"); ok {
			rmsg.ParentID = replyTo
		} else if replyTo, ok := event.Tags.Get("+draft/reply"); ok {
			rmsg.ParentID = replyTo
		}
	}

	// set action event
	if ok, ctcp := event.IsCTCP(); ok {
		if ctcp.Command != girc.CTCP_ACTION {
			b.Log.Debugf("dropping user ctcp, command: %s", ctcp.Command)
			return
		}
		rmsg.Event = config.EventUserAction
	}

	// set NOTICE event
	if event.Command == "NOTICE" {
		rmsg.Event = config.EventNoticeIRC
	}

	// strip action, we made an event if it was an action
	rmsg.Text += event.StripAction()

	converted, err := b.convertCharset(rmsg.Text)
	if err != nil {
		return
	}
	rmsg.Text = converted

	b.Log.Debugf("<= Sending message from %s on %s to gateway", event.Params[0], b.Account)
	b.Remote <- rmsg
}

// convertCharset normalizes inbound text into UTF-8. Returns the original
// text on detection failure so callers may still choose to relay it.
func (b *Birc) convertCharset(text string) (string, error) {
	mycharset := b.GetString("Charset")
	if mycharset == "" {
		detector := chardet.NewTextDetector()
		result, err := detector.DetectBest([]byte(text))
		if err != nil {
			b.Log.Infof("detection failed for text: %#v", text)
			return text, err
		}
		b.Log.Debugf("detected %s confidence %#v", result.Charset, result.Confidence)
		mycharset = result.Charset
		// Low confidence detection: fall back to a permissive single-byte
		// codec rather than risk mojibake from a guess.
		if result.Confidence < 80 {
			mycharset = "ISO-8859-1"
		}
	}
	switch mycharset {
	case "gbk", "gb18030", "gb2312", "big5", "euc-kr", "euc-jp", "shift-jis", "iso-2022-jp":
		return toUTF8(b.GetString("Charset"), text), nil
	default:
		r, err := charset.NewReader(mycharset, strings.NewReader(text))
		if err != nil {
			b.Log.Errorf("charset to utf-8 conversion failed: %s", err)
			return text, err
		}
		output, _ := ioutil.ReadAll(r)
		return string(output), nil
	}
}

// handleBatch only tracks draft/multiline batches; other batch types (e.g.
// chathistory) fall through and let handlePrivMsg handle each line as before.
func (b *Birc) handleBatch(client *girc.Client, event girc.Event) {
	if len(event.Params) == 0 {
		return
	}
	ref := event.Params[0]
	if len(ref) < 2 {
		return
	}
	refTag := ref[1:]
	switch ref[0] {
	case '+':
		var batchType, target string
		if len(event.Params) >= 2 {
			batchType = event.Params[1]
		}
		if len(event.Params) >= 3 {
			target = event.Params[2]
		}
		if batchType != multilineBatchType {
			return
		}
		mb := &multilineBatch{target: strings.ToLower(target)}
		if msgid, ok := event.Tags.Get("msgid"); ok {
			mb.msgid = msgid
		}
		if replyTo, ok := event.Tags.Get("+reply"); ok {
			mb.parentID = replyTo
		} else if replyTo, ok := event.Tags.Get("+draft/reply"); ok {
			mb.parentID = replyTo
		}
		b.multiline.start(refTag, mb)
	case '-':
		b.multiline.end(refTag)
	}
}

// appendMultilinePart returns false when the ref tag is unknown so the
// caller can fall back to normal per-line handling (chathistory batches,
// non-multiline batches, or any tag we never saw a start for).
func (b *Birc) appendMultilinePart(refTag string, event girc.Event) bool {
	_, concat := event.Tags.Get("draft/multiline-concat")
	text := event.Last()
	isAction := false
	if ok, ctcp := event.IsCTCP(); ok {
		if ctcp.Command != girc.CTCP_ACTION {
			return false
		}
		isAction = true
		text = event.StripAction()
	}
	isNotice := event.Command == "NOTICE"
	source := ""
	userID := ""
	if event.Source != nil {
		source = event.Source.Name
		userID = event.Source.Ident + "@" + event.Source.Host
	}
	return b.multiline.append(refTag, multilinePart{text: text, concat: concat}, source, userID, isAction, isNotice)
}

func (b *Birc) flushMultilineBatch(mb *multilineBatch) {
	if mb == nil || len(mb.parts) == 0 {
		return
	}
	// Drop self-echo: nothing to relay back to ourselves.
	if mb.source != "" && mb.source == b.Nick {
		return
	}
	text := combineMultiline(mb.parts)
	converted, err := b.convertCharset(text)
	if err == nil {
		text = converted
	}
	rmsg := config.Message{
		Username: mb.source,
		Channel:  mb.target,
		Account:  b.Account,
		UserID:   mb.userID,
		Avatar:   b.avatarURLFor(mb.source),
		Text:     text,
	}
	if b.GetBool("PreserveThreading") {
		rmsg.ID = mb.msgid
		rmsg.ParentID = mb.parentID
	}
	switch {
	case mb.isAction:
		rmsg.Event = config.EventUserAction
	case mb.isNotice:
		rmsg.Event = config.EventNoticeIRC
	}
	if mb.source != "" {
		b.requestAvatarOnce(mb.source)
	}
	b.Log.Debugf("<= Sending multiline batch (%d parts) from %s on %s to gateway", len(mb.parts), mb.source, mb.target)
	b.Remote <- rmsg
}

func (b *Birc) handleRunCommands() {
	for _, cmd := range b.GetStringSlice("RunCommands") {
		cmd = strings.ReplaceAll(cmd, "{BOTNICK}", b.Nick)
		if err := b.i.Cmd.SendRaw(cmd); err != nil {
			b.Log.Errorf("RunCommands %s failed: %s", cmd, err)
		}
		time.Sleep(time.Second)
	}
}

func (b *Birc) handleTopicWhoTime(client *girc.Client, event girc.Event) {
	parts := strings.Split(event.Params[2], "!")
	t, err := strconv.ParseInt(event.Params[3], 10, 64)
	if err != nil {
		b.Log.Errorf("Invalid time stamp: %s", event.Params[3])
	}
	user := parts[0]
	if len(parts) > 1 {
		user += " [" + parts[1] + "]"
	}
	b.Log.Debugf("%s: Topic set by %s [%s]", event.Command, user, time.Unix(t, 0))
}

// trackUserInChannel tracks users joining/leaving channels for QUIT event handling
func (b *Birc) trackUserInChannel(username, channel, command string) {
	if b.channelUsers[channel] == nil {
		b.channelUsers[channel] = make(map[string]bool)
	}

	switch command {
	case "JOIN":
		b.channelUsers[channel][username] = true
		b.Log.Debugf("Tracking user %s joining channel %s", username, channel)
	case "PART", "KICK":
		delete(b.channelUsers[channel], username)
		b.Log.Debugf("Tracking user %s leaving channel %s", username, channel)
	}
}
