package ssh

import "testing"

// Exercise the exact channel dispatch boundary behind CVE-2026-56855 and
// CVE-2026-78662, without requiring a live host or cloud credentials.
func TestPastureStackUndecidedChannelDropsTraffic(t *testing.T) {
	ch := &channel{incomingRequests: make(chan *Request, 1)}
	packet := Marshal(channelRequestMsg{Request: "exec", WantReply: true})
	if err := ch.handlePacket(packet); err != nil {
		t.Fatal(err)
	}
	if len(ch.incomingRequests) != 0 {
		t.Fatal("unestablished channel accepted traffic and can deadlock the mux")
	}
	ch.established.Store(true)
	if err := ch.handlePacket(packet); err != nil {
		t.Fatal(err)
	}
	if len(ch.incomingRequests) != 1 || (<-ch.incomingRequests).Type != "exec" {
		t.Fatal("normal requests must remain usable after channel establishment")
	}
}

func TestPastureStackEstablishedChannelRejectsUnexpectedMessages(t *testing.T) {
	ch := &channel{msg: make(chan interface{}, 1)}
	ch.established.Store(true)
	if err := ch.handlePacket(Marshal(globalRequestMsg{Type: "unexpected"})); err == nil {
		t.Fatal("unexpected messages must fail instead of filling the pending queue")
	}
	if len(ch.msg) != 0 {
		t.Fatal("unexpected traffic entered the channel queue")
	}
	ch.sentRequestPending.Store(true)
	if err := ch.handlePacket(Marshal(channelRequestSuccessMsg{})); err != nil {
		t.Fatal(err)
	}
	if len(ch.msg) != 1 {
		t.Fatal("the legitimate pending request response was lost")
	}
}
