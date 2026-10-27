package coderd

import (
	"context"
	"io"
	"time"

	"golang.org/x/xerrors"

	"github.com/coder/coder/v2/codersdk"
	"github.com/coder/quartz"
)

const (
	// chatStreamPacerTarget is how long the bytes queued ahead of a ping
	// may take to drain. WSWatcher fails a probe whose ping takes over
	// 15 s, of which the lock wait behind a message may use 5 s; 4 s leaves
	// room for the round trip and for the rate to fall by about half.
	chatStreamPacerTarget = 4 * time.Second
	// chatStreamPacerMinWindow is the largest chunk message, which the
	// pacer writes on its own when nothing is unconfirmed.
	chatStreamPacerMinWindow = 2 * codersdk.ChatStreamChunkBytes
)

// chatStreamPinger sends a WebSocket ping and returns when the pong
// arrives. *websocket.Conn implements it.
type chatStreamPinger interface {
	Ping(ctx context.Context) error
}

// chatStreamPacer bounds the chat stream bytes queued ahead of a
// WSWatcher ping to what the link drains in chatStreamPacerTarget, so a
// slow link with deep buffers does not fail the probe. A batch that fits
// in the window is not delayed.
//
// Write writes at most window bytes between pongs. Before a write that
// would pass the window, it pings and waits for the pong. The writer is
// blocked while the ping frame is written, so the pong confirms every byte
// written before it and the path holds no stream data when Write
// continues. The next window is the bytes confirmed by the pong divided by
// the time since the previous pong, times the target. Those bytes were all
// written and read within that time, so the measured rate never exceeds
// the delivery rate, and no growth cap is needed. Idle time lowers the
// measurement, and the window returns to its minimum.
//
// It assumes it is the only writer of data messages, that Write is not
// called concurrently, and that the delivery rate does not fall by more
// than half within one window. While the window limits a stream, the
// stream gets the delivery rate times (1 - RTT/target), because the link
// idles for one round trip per window, and the window grows by about
// target/RTT per round trip from its minimum.
//
// Through a throttled proxy, a 10 MiB image arrived in one attempt at
// 1 Mbit/s with a 1 MiB proxy queue or 64 KiB TCP segments, where it never
// arrived without the pacer, and in 28 s at 4 Mbit/s. At 100 Mbit/s the
// pacer added 0.2 s at 100 ms RTT and 1.9 s at 600 ms RTT.
//
// ctx is the stream's context; a pacer lives for one stream.
type chatStreamPacer struct {
	ctx    context.Context
	w      io.Writer
	pinger chatStreamPinger
	clock  quartz.Clock

	// unconfirmed counts bytes written since the last pong.
	unconfirmed int64
	window      int64
	lastPong    time.Time
}

func newChatStreamPacer(ctx context.Context, w io.Writer, pinger chatStreamPinger, clock quartz.Clock) *chatStreamPacer {
	return &chatStreamPacer{
		ctx:      ctx,
		w:        w,
		pinger:   pinger,
		clock:    clock,
		window:   chatStreamPacerMinWindow,
		lastPong: clock.Now("chatStreamPacer", "start"),
	}
}

// Write writes b as one message after waiting for a pong when b would
// pass the window.
func (p *chatStreamPacer) Write(b []byte) (int, error) {
	if p.unconfirmed > 0 && p.unconfirmed+int64(len(b)) > p.window {
		if err := p.pinger.Ping(p.ctx); err != nil {
			return 0, xerrors.Errorf("wait for chat stream client to read: %w", err)
		}
		now := p.clock.Now("chatStreamPacer", "pong")
		elapsed := now.Sub(p.lastPong)
		p.window = chatStreamPacerMinWindow
		if elapsed > 0 {
			rate := float64(p.unconfirmed) / elapsed.Seconds()
			p.window = max(p.window, int64(rate*chatStreamPacerTarget.Seconds()))
		}
		p.unconfirmed = 0
		p.lastPong = now
	}
	n, err := p.w.Write(b)
	p.unconfirmed += int64(n)
	return n, err
}
