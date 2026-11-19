package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/olahol/melody"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/42wim/matterbridge/bridge"
	"github.com/42wim/matterbridge/bridge/config"
)

func newTestAPI() (*API, chan config.Message) {
	return newTestAPIWithConfig("")
}

func newTestAPIWithConfig(toml string) (*API, chan config.Message) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	remote := make(chan config.Message, 1)
	b := &API{
		Config: &bridge.Config{
			Bridge: &bridge.Bridge{
				Account: "api.test",
				Log:     logrus.NewEntry(logger),
				Config:  config.NewConfigFromString(logger, []byte(toml)),
			},
			Remote: remote,
		},
		mrouter:   melody.New(),
		postedIDs: newPostedIDs(),
	}
	return b, remote
}

func postMessage(t *testing.T, b *API, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/message", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	err := b.handlePostMessage(echo.New().NewContext(req, rec))
	if httpErr, ok := err.(*echo.HTTPError); ok {
		rec.Code = httpErr.Code
		return rec
	}
	require.NoError(t, err)
	return rec
}

func TestHandlePostMessageAssignsID(t *testing.T) {
	b, remote := newTestAPI()
	postMessage(t, b, `{"text":"hi","username":"bob","id":"client-chosen"}`)
	msg := <-remote
	assert.NotEqual(t, "client-chosen", msg.ID)
	assert.NotEmpty(t, msg.ID)
}

func TestHandlePostMessageDeleteKeepsID(t *testing.T) {
	b, remote := newTestAPI()
	postMessage(t, b, `{"text":"hi","username":"bob"}`)
	postedID := (<-remote).ID
	postMessage(t, b, `{"event":"msg_delete","id":"`+postedID+`"}`)
	msg := <-remote
	assert.Equal(t, postedID, msg.ID)
	assert.Equal(t, config.EventMsgDelete, msg.Event)
	assert.Equal(t, config.EventMsgDelete, msg.Text)
}

func TestHandlePostMessageDeleteNeedsOwnID(t *testing.T) {
	b, remote := newTestAPI()
	rec := postMessage(t, b, `{"event":"msg_delete","id":"posted-elsewhere"}`)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Empty(t, remote)
}

func TestSendSkipsDeletesUnlessRelayDeletes(t *testing.T) {
	deleted := config.Message{Event: config.EventMsgDelete, ID: "irc abc"}

	b, _ := newTestAPIWithConfig("")
	_, err := b.Send(deleted)
	require.NoError(t, err)
	assert.Empty(t, b.Messages.Values())

	b, _ = newTestAPIWithConfig("[api.test]\nRelayDeletes=true\n")
	_, err = b.Send(deleted)
	require.NoError(t, err)
	assert.Len(t, b.Messages.Values(), 1)
}

func TestHandlePostMessageDeleteNeedsID(t *testing.T) {
	b, remote := newTestAPI()
	rec := postMessage(t, b, `{"event":"msg_delete"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Empty(t, remote)
}
