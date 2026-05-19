package birc

import (
	"strings"
	"sync"
	"time"
)

// IRCv3 draft/multiline (https://ircv3.net/specs/extensions/multiline) bundles
// many PRIVMSGs inside a BATCH so the original logical message survives the
// IRC 512-byte line limit. Inner lines are joined with `\n`; a per-line
// `draft/multiline-concat` tag means the previous line was hard-wrapped and
// must be re-joined with no separator.

const (
	// multilineBatchType is the BATCH start type indicating an IRCv3
	// draft/multiline batch (we ignore other batch types e.g. chathistory).
	multilineBatchType = "draft/multiline"

	// multilineBatchTimeout is the safety flush window: if a BATCH end
	// never arrives (server bug, disconnect mid-batch) we still relay
	// what we have rather than leaking memory and dropping the message.
	multilineBatchTimeout = 10 * time.Second
)

type multilinePart struct {
	text   string
	concat bool
}

type multilineBatch struct {
	target   string
	msgid    string
	parentID string
	source   string
	userID   string
	isAction bool
	isNotice bool
	parts    []multilinePart

	timer *time.Timer
	done  bool
}

// combineMultiline joins parts per spec: `\n` separator by default,
// no separator when the next line carries draft/multiline-concat.
func combineMultiline(parts []multilinePart) string {
	if len(parts) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(parts[0].text)
	for i := 1; i < len(parts); i++ {
		if !parts[i].concat {
			sb.WriteByte('\n')
		}
		sb.WriteString(parts[i].text)
	}
	return sb.String()
}

// batchManager guarantees onFlush fires exactly once per batch — on the
// BATCH end OR on the safety timeout — so a dropped end can't strand a
// message in memory.
type batchManager struct {
	mu      sync.Mutex
	batches map[string]*multilineBatch
	timeout time.Duration
	onFlush func(*multilineBatch)
}

func newBatchManager(timeout time.Duration, onFlush func(*multilineBatch)) *batchManager {
	return &batchManager{
		batches: make(map[string]*multilineBatch),
		timeout: timeout,
		onFlush: onFlush,
	}
}

// start registers a draft/multiline batch. The timer guarantees eventual
// delivery if the BATCH end is lost.
func (m *batchManager) start(refTag string, b *multilineBatch) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.batches[refTag]; exists {
		return
	}
	b.timer = time.AfterFunc(m.timeout, func() { m.flush(refTag) })
	m.batches[refTag] = b
}

// append adds a part to the batch. Returns false when refTag is unknown
// so the caller can fall back to its non-batch path.
func (m *batchManager) append(refTag string, part multilinePart, source, userID string, isAction, isNotice bool) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.batches[refTag]
	if !ok || b.done {
		return false
	}
	if len(b.parts) == 0 {
		b.source = source
		b.userID = userID
		b.isAction = isAction
		b.isNotice = isNotice
	}
	b.parts = append(b.parts, part)
	return true
}

func (m *batchManager) end(refTag string) {
	m.flush(refTag)
}

func (m *batchManager) flush(refTag string) {
	m.mu.Lock()
	b, ok := m.batches[refTag]
	if !ok || b.done {
		m.mu.Unlock()
		return
	}
	b.done = true
	delete(m.batches, refTag)
	if b.timer != nil {
		b.timer.Stop()
	}
	m.mu.Unlock()
	if m.onFlush != nil {
		m.onFlush(b)
	}
}

func (m *batchManager) active(refTag string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.batches[refTag]
	return ok
}
