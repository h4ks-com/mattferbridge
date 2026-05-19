package birc

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lrstanley/girc"
)

const testChan = "#bots"

func TestCombineMultiline(t *testing.T) {
	cases := []struct {
		name  string
		parts []multilinePart
		want  string
	}{
		{
			name:  "empty",
			parts: nil,
			want:  "",
		},
		{
			name:  "single line",
			parts: []multilinePart{{text: "hello"}},
			want:  "hello",
		},
		{
			name: "two lines no concat",
			parts: []multilinePart{
				{text: "line 1"},
				{text: "line 2"},
			},
			want: "line 1\nline 2",
		},
		{
			name: "blank line preserved",
			parts: []multilinePart{
				{text: "line 1"},
				{text: ""},
				{text: "line 3"},
			},
			want: "line 1\n\nline 3",
		},
		{
			name: "concat joins without newline",
			parts: []multilinePart{
				{text: "how is "},
				{text: "everyone?", concat: true},
			},
			want: "how is everyone?",
		},
		{
			name: "mixed concat and newline",
			parts: []multilinePart{
				{text: "spec"},
				{text: "says: "},
				{text: "blank ", concat: true},
				{text: "next"},
			},
			want: "spec\nsays: blank \nnext",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := combineMultiline(tc.parts)
			if got != tc.want {
				t.Fatalf("combineMultiline = %q, want %q", got, tc.want)
			}
		})
	}
}

// captureFlush returns an onFlush callback that records every flush into a
// slice and signals on flushed.
type captureFlush struct {
	mu       sync.Mutex
	got      []*multilineBatch
	flushed  chan struct{}
	flushCnt int32
}

func newCapture() *captureFlush {
	return &captureFlush{flushed: make(chan struct{}, 32)}
}

func (c *captureFlush) cb(b *multilineBatch) {
	c.mu.Lock()
	c.got = append(c.got, b)
	c.mu.Unlock()
	atomic.AddInt32(&c.flushCnt, 1)
	select {
	case c.flushed <- struct{}{}:
	default:
	}
}

func (c *captureFlush) calls() int { return int(atomic.LoadInt32(&c.flushCnt)) }

func TestBatchManager_NormalEnd(t *testing.T) {
	capt := newCapture()
	m := newBatchManager(2*time.Second, capt.cb)
	m.start("abc", &multilineBatch{target: testChan, msgid: "id1"})
	if !m.active("abc") {
		t.Fatal("expected abc to be active after start")
	}
	if !m.append("abc", multilinePart{text: "hi"}, "alice", "a@h", false, false) {
		t.Fatal("append should succeed on active batch")
	}
	m.append("abc", multilinePart{text: "there"}, "alice", "a@h", false, false)
	m.end("abc")

	if capt.calls() != 1 {
		t.Fatalf("expected 1 flush, got %d", capt.calls())
	}
	if m.active("abc") {
		t.Fatal("batch should be removed after flush")
	}
	if got := capt.got[0]; got.source != "alice" || got.target != testChan || got.msgid != "id1" {
		t.Fatalf("unexpected batch metadata: %+v", got)
	}
	if got := combineMultiline(capt.got[0].parts); got != "hi\nthere" {
		t.Fatalf("combined = %q", got)
	}
}

func TestBatchManager_TimeoutFlush(t *testing.T) {
	capt := newCapture()
	m := newBatchManager(50*time.Millisecond, capt.cb)
	m.start("late", &multilineBatch{target: testChan})
	m.append("late", multilinePart{text: "stranded"}, "bob", "b@h", false, false)

	select {
	case <-capt.flushed:
	case <-time.After(time.Second):
		t.Fatal("timeout flush never fired")
	}
	if capt.calls() != 1 {
		t.Fatalf("expected 1 flush from timeout, got %d", capt.calls())
	}
}

func TestBatchManager_NoDoubleFlush(t *testing.T) {
	capt := newCapture()
	m := newBatchManager(20*time.Millisecond, capt.cb)
	m.start("dup", &multilineBatch{target: "#x"})
	m.append("dup", multilinePart{text: "one"}, "u", "u@h", false, false)
	m.end("dup")
	// Wait past the safety timeout: the timer must have been stopped.
	time.Sleep(60 * time.Millisecond)

	if capt.calls() != 1 {
		t.Fatalf("expected single flush even after timer window, got %d", capt.calls())
	}
}

func TestBatchManager_UnknownRef(t *testing.T) {
	capt := newCapture()
	m := newBatchManager(time.Second, capt.cb)
	if m.append("nope", multilinePart{text: "x"}, "u", "u@h", false, false) {
		t.Fatal("append on unknown ref must return false")
	}
	m.end("nope") // must not panic; no flush
	if capt.calls() != 0 {
		t.Fatalf("unknown end should not flush, got %d", capt.calls())
	}
}

func TestBatchManager_DuplicateStartIgnored(t *testing.T) {
	capt := newCapture()
	m := newBatchManager(time.Second, capt.cb)
	first := &multilineBatch{target: "#a", msgid: "first"}
	second := &multilineBatch{target: "#a", msgid: "second"}
	m.start("ref", first)
	m.start("ref", second) // ignored
	m.append("ref", multilinePart{text: "hi"}, "u", "u@h", false, false)
	m.end("ref")
	if capt.calls() != 1 {
		t.Fatalf("expected 1 flush, got %d", capt.calls())
	}
	if capt.got[0].msgid != "first" {
		t.Fatalf("second start must not replace first; msgid=%q", capt.got[0].msgid)
	}
}

func TestBatchManager_ConcurrentBatches(t *testing.T) {
	capt := newCapture()
	m := newBatchManager(time.Second, capt.cb)
	m.start("A", &multilineBatch{target: "#a"})
	m.start("B", &multilineBatch{target: "#b"})
	m.append("A", multilinePart{text: "a1"}, "ua", "ua@h", false, false)
	m.append("B", multilinePart{text: "b1"}, "ub", "ub@h", false, false)
	m.append("A", multilinePart{text: "a2"}, "ua", "ua@h", false, false)
	m.end("B")
	m.end("A")

	if capt.calls() != 2 {
		t.Fatalf("expected 2 flushes, got %d", capt.calls())
	}
	// onFlush ordering reflects end() ordering: B then A.
	if capt.got[0].target != "#b" || capt.got[1].target != "#a" {
		t.Fatalf("flush order wrong: %q then %q", capt.got[0].target, capt.got[1].target)
	}
	if got := combineMultiline(capt.got[1].parts); got != "a1\na2" {
		t.Fatalf("A combined = %q", got)
	}
}

// TestParseRealBatchWire fixes the wire format expectations against captured
// bytes from irc.h4ks.com (.hanb response from _cloudbot). If girc's parsing
// or our tag/param indexing ever shifts, this test catches it.
func TestParseRealBatchWire(t *testing.T) { //nolint:gocyclo
	startRaw := `@time=2026-05-19T15:58:12.075Z;msgid=w6ipczargzsews2j8bufqyg4a2;bot;+draft/reply=t5yni59kwxaes9n96uzx49az2a :_cloudbot!~u@k8uz6kene5usn.irc BATCH +hr59ytyqrrgcq draft/multiline :#bots`
	innerRaw := `@batch=hr59ytyqrrgcq :_cloudbot!~u@k8uz6kene5usn.irc PRIVMSG #bots :⬜⬜⬜⬜⬜⬜⬜⬜⬛`
	endRaw := `:_cloudbot!~u@k8uz6kene5usn.irc BATCH :-hr59ytyqrrgcq`

	start := girc.ParseEvent(startRaw)
	if start == nil || start.Command != "BATCH" {
		t.Fatalf("expected BATCH command, got %#v", start)
	}
	if len(start.Params) < 3 || start.Params[0] != "+hr59ytyqrrgcq" || start.Params[1] != "draft/multiline" || start.Params[2] != "#bots" {
		t.Fatalf("BATCH start params = %#v", start.Params)
	}
	if msgid, ok := start.Tags.Get("msgid"); !ok || msgid != "w6ipczargzsews2j8bufqyg4a2" {
		t.Fatalf("BATCH start msgid tag = %q ok=%v", msgid, ok)
	}
	if reply, ok := start.Tags.Get("+draft/reply"); !ok || reply != "t5yni59kwxaes9n96uzx49az2a" {
		t.Fatalf("BATCH start +draft/reply tag = %q ok=%v", reply, ok)
	}

	inner := girc.ParseEvent(innerRaw)
	if inner == nil || inner.Command != "PRIVMSG" {
		t.Fatalf("expected PRIVMSG, got %#v", inner)
	}
	refTag, ok := inner.Tags.Get("batch")
	if !ok || refTag != "hr59ytyqrrgcq" {
		t.Fatalf("inner batch tag = %q ok=%v", refTag, ok)
	}
	if got := inner.Last(); !strings.HasPrefix(got, "⬜") {
		t.Fatalf("inner Last() = %q", got)
	}

	end := girc.ParseEvent(endRaw)
	if end == nil || end.Command != "BATCH" {
		t.Fatalf("expected BATCH end, got %#v", end)
	}
	if len(end.Params) < 1 || end.Params[0] != "-hr59ytyqrrgcq" {
		t.Fatalf("BATCH end params = %#v", end.Params)
	}
}

func TestBatchManager_FirstPartCarriesMetadata(t *testing.T) {
	capt := newCapture()
	m := newBatchManager(time.Second, capt.cb)
	m.start("meta", &multilineBatch{target: "#c"})
	// First part is an action; subsequent flags must not overwrite source.
	m.append("meta", multilinePart{text: "waves"}, "first", "f@h", true, false)
	m.append("meta", multilinePart{text: "again"}, "second", "s@h", false, false)
	m.end("meta")

	got := capt.got[0]
	if got.source != "first" || got.userID != "f@h" || !got.isAction {
		t.Fatalf("first-part metadata not preserved: %+v", got)
	}
}

// newBircForBatchTest builds the minimum Birc state needed to exercise the
// batch wiring without a real IRC connection or gateway dependency.
func newBircForBatchTest(t *testing.T, timeout time.Duration) (*Birc, <-chan *multilineBatch) {
	t.Helper()
	ch := make(chan *multilineBatch, 4)
	b := &Birc{}
	b.multiline = newBatchManager(timeout, func(mb *multilineBatch) { ch <- mb })
	return b, ch
}

func TestHandleBatch_FullCycle(t *testing.T) {
	b, flushed := newBircForBatchTest(t, time.Second)

	start := girc.ParseEvent(`@msgid=ABC123 :alice!u@h BATCH +xyz draft/multiline :#bots`)
	if start == nil {
		t.Fatal("parse start")
	}
	b.handleBatch(nil, *start)

	inner1 := girc.ParseEvent(`@batch=xyz :alice!u@h PRIVMSG #bots :line one`)
	if !b.appendMultilinePart("xyz", *inner1) {
		t.Fatal("expected first inner to be claimed")
	}
	inner2 := girc.ParseEvent(`@batch=xyz :alice!u@h PRIVMSG #bots :line two`)
	b.appendMultilinePart("xyz", *inner2)

	end := girc.ParseEvent(`:alice!u@h BATCH :-xyz`)
	b.handleBatch(nil, *end)

	select {
	case mb := <-flushed:
		if got := combineMultiline(mb.parts); got != "line one\nline two" {
			t.Fatalf("combined = %q", got)
		}
		if mb.msgid != "ABC123" {
			t.Fatalf("msgid = %q", mb.msgid)
		}
		if mb.target != testChan {
			t.Fatalf("target = %q", mb.target)
		}
		if mb.source != "alice" || mb.userID != "u@h" {
			t.Fatalf("source/userID = %q/%q", mb.source, mb.userID)
		}
	case <-time.After(time.Second):
		t.Fatal("no flush after BATCH end")
	}
}

func TestHandleBatch_ConcatTag(t *testing.T) {
	b, flushed := newBircForBatchTest(t, time.Second)
	b.handleBatch(nil, *girc.ParseEvent(`:s!u@h BATCH +c1 draft/multiline :#x`))
	b.appendMultilinePart("c1", *girc.ParseEvent(`@batch=c1 :s!u@h PRIVMSG #x :how is `))
	b.appendMultilinePart("c1", *girc.ParseEvent(`@batch=c1;draft/multiline-concat :s!u@h PRIVMSG #x :everyone?`))
	b.handleBatch(nil, *girc.ParseEvent(`:s!u@h BATCH :-c1`))

	mb := <-flushed
	if got := combineMultiline(mb.parts); got != "how is everyone?" {
		t.Fatalf("concat join = %q", got)
	}
}

func TestHandleBatch_ChathistoryFallsThrough(t *testing.T) {
	b, flushed := newBircForBatchTest(t, 200*time.Millisecond)
	// chathistory BATCH start: handleBatch must ignore it.
	b.handleBatch(nil, *girc.ParseEvent(`:s BATCH +ch1 chathistory :#x`))
	if b.multiline.active("ch1") {
		t.Fatal("chathistory batch should not be tracked")
	}
	// Inner PRIVMSG carries @batch=ch1; appendMultilinePart must return
	// false so the caller falls back to per-line handling.
	if b.appendMultilinePart("ch1", *girc.ParseEvent(`@batch=ch1 :s!u@h PRIVMSG #x :replayed`)) {
		t.Fatal("must not claim chathistory line")
	}
	select {
	case mb := <-flushed:
		t.Fatalf("unexpected flush: %+v", mb)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestHandleBatch_ActionFirstLineCarriesEvent(t *testing.T) {
	b, flushed := newBircForBatchTest(t, time.Second)
	b.handleBatch(nil, *girc.ParseEvent(`:s!u@h BATCH +act draft/multiline :#x`))
	b.appendMultilinePart("act", *girc.ParseEvent("@batch=act :s!u@h PRIVMSG #x :\x01ACTION waves slowly\x01"))
	b.appendMultilinePart("act", *girc.ParseEvent(`@batch=act :s!u@h PRIVMSG #x :and continues`))
	b.handleBatch(nil, *girc.ParseEvent(`:s BATCH :-act`))

	mb := <-flushed
	if !mb.isAction {
		t.Fatal("first-line ACTION must propagate to batch")
	}
	if got := combineMultiline(mb.parts); got != "waves slowly\nand continues" {
		t.Fatalf("combined = %q", got)
	}
}

func TestHandleBatch_DuplicateEndSafe(t *testing.T) {
	b, flushed := newBircForBatchTest(t, time.Second)
	b.handleBatch(nil, *girc.ParseEvent(`:s!u@h BATCH +d draft/multiline :#x`))
	b.appendMultilinePart("d", *girc.ParseEvent(`@batch=d :s!u@h PRIVMSG #x :hi`))
	b.handleBatch(nil, *girc.ParseEvent(`:s BATCH :-d`))
	// A spurious second end (server bug or duplicate event) must not panic
	// or flush twice.
	b.handleBatch(nil, *girc.ParseEvent(`:s BATCH :-d`))

	<-flushed
	select {
	case <-flushed:
		t.Fatal("second flush from duplicate end")
	case <-time.After(50 * time.Millisecond):
	}
}
