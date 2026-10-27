package coderd

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/xerrors"

	"github.com/coder/quartz"
)

// fakeChatStreamLink delivers written bytes at rate bytes per second and
// answers a ping one round trip after the bytes queued ahead of it drain.
type fakeChatStreamLink struct {
	clock   *quartz.Mock
	rate    float64
	rtt     time.Duration
	queued  float64
	updated time.Time
	pings   int
	// maxQueued is the most bytes queued at once, which a WSWatcher ping
	// sent at that moment would wait behind.
	maxQueued float64
	pingErr   error
}

func (l *fakeChatStreamLink) drain() {
	now := l.clock.Now()
	l.queued = max(0, l.queued-l.rate*now.Sub(l.updated).Seconds())
	l.updated = now
}

func (l *fakeChatStreamLink) Write(b []byte) (int, error) {
	l.drain()
	l.queued += float64(len(b))
	l.maxQueued = max(l.maxQueued, l.queued)
	return len(b), nil
}

func (l *fakeChatStreamLink) Ping(context.Context) error {
	if l.pingErr != nil {
		return l.pingErr
	}
	l.drain()
	l.pings++
	wait := time.Duration(l.queued/l.rate*float64(time.Second)) + l.rtt
	l.clock.Advance(wait)
	l.drain()
	return nil
}

func newFakeChatStreamLink(t *testing.T, rate float64, rtt time.Duration) (*fakeChatStreamLink, *chatStreamPacer) {
	clock := quartz.NewMock(t)
	link := &fakeChatStreamLink{clock: clock, rate: rate, rtt: rtt, updated: clock.Now()}
	return link, newChatStreamPacer(context.Background(), link, link, clock)
}

func writeChatStreamMessages(t *testing.T, p *chatStreamPacer, count, size int) {
	t.Helper()
	msg := make([]byte, size)
	for range count {
		_, err := p.Write(msg)
		require.NoError(t, err)
	}
}

func TestChatStreamPacer(t *testing.T) {
	t.Parallel()

	t.Run("WindowFromPong", func(t *testing.T) {
		t.Parallel()

		link, p := newFakeChatStreamLink(t, 300_000, 100*time.Millisecond)
		writeChatStreamMessages(t, p, 2, 300_000)
		require.Equal(t, 1, link.pings)
		// 300 kB drained in 1 s, plus the round trip.
		confirmed := float64(300_000)
		require.EqualValues(t, int64(confirmed/1.1*4), p.window)
	})

	t.Run("MessageLargerThanWindow", func(t *testing.T) {
		t.Parallel()

		link, p := newFakeChatStreamLink(t, 1<<20, 100*time.Millisecond)
		writeChatStreamMessages(t, p, 1, 4*chatStreamPacerMinWindow)
		require.Zero(t, link.pings, "nothing is unconfirmed, so the message goes out alone")
		writeChatStreamMessages(t, p, 1, 1)
		require.Equal(t, 1, link.pings)
		start := link.clock.Now()
		writeChatStreamMessages(t, p, 1, 1)
		require.Equal(t, 1, link.pings, "a write within the window does not ping")
		require.Equal(t, start, link.clock.Now())
	})

	t.Run("IdleLowersWindow", func(t *testing.T) {
		t.Parallel()

		link, p := newFakeChatStreamLink(t, 10<<20, 100*time.Millisecond)
		writeChatStreamMessages(t, p, 40, 256<<10)
		before := p.window
		// The bytes confirmed by the next pong were written over a minute.
		link.clock.Advance(time.Minute)
		pings := link.pings
		for i := 0; i < 1000 && link.pings == pings; i++ {
			writeChatStreamMessages(t, p, 1, 256<<10)
		}
		require.Greater(t, link.pings, pings)
		require.Less(t, p.window, before/10)
	})

	t.Run("QueueDrainsWithinTarget", func(t *testing.T) {
		t.Parallel()

		for _, tc := range []struct {
			name string
			rate float64
			rtt  time.Duration
		}{
			{"1Mbps", 1e6 / 8, 20 * time.Millisecond},
			{"4Mbps", 4e6 / 8, 20 * time.Millisecond},
			{"100Mbps600ms", 100e6 / 8, 600 * time.Millisecond},
		} {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				link, p := newFakeChatStreamLink(t, tc.rate, tc.rtt)
				start := link.clock.Now()
				writeChatStreamMessages(t, p, 2000, 256<<10)
				require.LessOrEqual(t, link.maxQueued, max(tc.rate*chatStreamPacerTarget.Seconds(), chatStreamPacerMinWindow))
				// Window-limited throughput is rate * (1 - rtt/target).
				minRate := tc.rate * (1 - tc.rtt.Seconds()/chatStreamPacerTarget.Seconds()) * 0.9
				elapsed := link.clock.Now().Sub(start).Seconds() + link.queued/tc.rate
				require.GreaterOrEqual(t, float64(2000*256<<10)/elapsed, minRate)
			})
		}
	})

	t.Run("PingError", func(t *testing.T) {
		t.Parallel()

		link, p := newFakeChatStreamLink(t, 1<<20, 0)
		link.pingErr = xerrors.New("closed")
		writeChatStreamMessages(t, p, 1, chatStreamPacerMinWindow)
		queued := link.queued
		_, err := p.Write([]byte("x"))
		require.ErrorIs(t, err, link.pingErr)
		require.Equal(t, queued, link.queued, "the message is not written")
	})
}
