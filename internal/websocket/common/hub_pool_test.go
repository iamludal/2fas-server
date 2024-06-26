package common

import (
	"fmt"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
)

func TestRegisterClientDoesNotCreateSameHubTwice(t *testing.T) {
	hp := newHubPool()

	const channelID = "channelID"

	_, h1 := hp.registerClient(channelID, &websocket.Conn{})
	_, h2 := hp.registerClient(channelID, &websocket.Conn{})

	if h1 != h2 {
		t.Fatal("New hub was created")
	}
}

func TestRemovingEmptyHub(t *testing.T) {
	hp := newHubPool()

	const channelID = "channelID"
	c, h1 := hp.registerClient(channelID, &websocket.Conn{})
	h1.unregisterClient(c)

	_, h2 := hp.registerClient(channelID, &websocket.Conn{})

	if !h1.isEmpty() {
		t.Fatalf("Hub does not report it is empty, even though it should")
	}
	if h1 == h2 {
		t.Fatal("Old hub wasn't deleted")
	}
	if h2.isEmpty() {
		t.Fatal("New heb is empty, even though it shouldn't")
	}
}

// TestCreateRemoveConcurrently in which we (for each channel) register a client and then unregister it immediately.
// The last client to be register stays that way.
// We then check:
// - if poll has non-empty hubs,
// - iff all hubs removed from the poll are empty.
func TestCreateRemoveConcurrently(t *testing.T) {
	hp := newHubPool()
	const channelsNo = 100
	const clientsPerChannel = 1000
	const messagesSentToEachHub = 100

	hubs := &sync.Map{}

	wg := sync.WaitGroup{}
	// First we create `channelsNo` goroutines. Each of them creates `clientsPerChannel` sub-goroutines.
	// This gives us `channelsNo*clientsPerChannel` sub go-routines and `channelsNo` parent goroutines.
	// Each of them will call `wg.Done() once and we can't progress until all of them are done.
	wg.Add(channelsNo*clientsPerChannel + channelsNo)
	// We will close `channelsNo*clientsPerChannel + channelsNo` clients. We create fakeReadPump for each of them and
	// wait for it to finish.
	wg.Add(channelsNo * clientsPerChannel)

	for i := 0; i < channelsNo; i++ {
		channelID := fmt.Sprintf("channel-%d", i)

		c, h := hp.registerClient(channelID, &websocket.Conn{})
		hubs.Store(h, struct{}{})
		go fakeReadPump(c.send, &wg)
		go func() {
			for i := 0; i < messagesSentToEachHub; i++ {
				h.broadcastMsg([]byte("test"))
			}
		}()

		go func() {
			defer wg.Done()
			for j := 0; j < clientsPerChannel; j++ {
				c, h := hp.registerClient(channelID, &websocket.Conn{})
				go fakeReadPump(c.send, &wg)

				go func() {
					h.unregisterClient(c)
					wg.Done()
				}()
			}
		}()
	}
	wg.Wait()

	for c, hub := range hp.hubs {
		if hub.isEmpty() {
			t.Fatalf("Empty hub found in channel: %q", c)
		}
	}

	hubs.Range(func(key, value any) bool {
		h1 := key.(*Hub)
		if !h1.isEmpty() {
			if h2, ok := hp.hubs[h1.id]; !ok || h1 != h2 {
				t.Fatalf("Non-empty hub was evicted from hub pool: %q", h1.id)
			}
		}
		return true
	})
}

func fakeReadPump(c chan []byte, wg *sync.WaitGroup) {
	defer wg.Done()
	for range c {
	}
}
