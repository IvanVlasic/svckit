package session

import (
	"context"
	"sync"
	"time"

	"github.com/minus5/svckit/amp"
	"github.com/minus5/svckit/log"
)

var (
	maxWriteQueueDepth = 128              // max number of messages in output write queue
	aliveInterval      = 32 * time.Second // interval for sending alive messages
)

type session struct {
	conn            connection      // client websocket connection
	broker          broker          // broker for subscribe on published messages
	requester       requester       // requester for request / response messages
	outQueue        []*amp.Msg      // output messages queue
	outQueueChanged chan (struct{}) // signal that queue changed
	stats           struct {        // sessions stats counters
		start         time.Time
		outMessages   int
		inMessages    int
		aliveMessages int
		maxQueueLen   int
	}
	sync.Mutex
}

// serve starts new session
// Blocks until session is finished.
func serve(cancelSig context.Context, conn connection, req requester, brk broker) {
	s := &session{
		conn:            conn,
		requester:       req,
		broker:          brk,
		outQueue:        make([]*amp.Msg, 0),
		outQueueChanged: make(chan struct{}),
	}
	s.stats.start = time.Now()
	s.loop(cancelSig)
}

func (s *session) loop(cancelSig context.Context) {
	inMessages := s.readLoop()            // messages from the client
	outMessages := make(chan *amp.Msg, 1) // messages to the client
	exitSig := cancelSig.Done()           // aplication exit signal

	// timer for alive messages
	alive := time.NewTimer(aliveInterval)
	// if there is no other messages send alive
	sendAlive := func() {
		s.Lock()
		defer s.Unlock()
		if len(s.outQueue) == 0 {
			s.outQueue = append(s.outQueue, amp.NewAlive())
			s.stats.aliveMessages++
		}
	}

	// if there is anything in queue waiting for sending put it inot outMessages chan
	tryPopQueue := func() {
		s.Lock()
		defer s.Unlock()
		if len(s.outQueue) > 0 {
			select { /// non blocking write
			case outMessages <- s.outQueue[0]:
				s.outQueue = s.outQueue[1:]
			default:
			}
		}
	}

	// log session stats
	defer func() {
		s.Lock()
		defer s.Unlock()
		s.log().I("inMessages", s.stats.inMessages).
			I("outMessages", s.stats.outMessages).
			I("aliveMessages", s.stats.aliveMessages).
			I("durationMs", int(time.Now().Sub(s.stats.start)/time.Millisecond)).
			Debug("stats")
	}()

	for {
		tryPopQueue()

		select {
		case <-s.outQueueChanged:
			// just start another loop iteration
		case <-alive.C:
			sendAlive()
		case msg := <-outMessages:
			s.connWrite(msg)
			alive.Reset(aliveInterval)
			s.stats.outMessages++
		case msg, ok := <-inMessages:
			if !ok {
				s.unsubscribe()
				return
			}
			s.receive(msg)
			s.stats.inMessages++
		case <-exitSig:
			_ = s.conn.Close()
			exitSig = nil // fire once
		}
	}
}

func (s *session) unsubscribe() {
	s.broker.Unsubscribe(s)
	s.requester.Unsubscribe(s)
}

func (s *session) readLoop() chan *amp.Msg {
	in := make(chan *amp.Msg)
	go func() {
		defer close(in)
		for {
			buf, err := s.conn.Read()
			if err != nil {
				return
			}
			if m := amp.Parse(buf); m != nil {
				in <- m
			}
		}
	}()
	return in
}

// receive gets client messages
func (s *session) receive(m *amp.Msg) {
	switch m.Type {
	case amp.Ping:
		s.Send(m.Pong())
	case amp.Request:
		// TODO what URI-a are ok, make filter
		s.requester.Send(s, m)
	case amp.Subscribe:
		s.broker.Subscribe(s, m.Subscriptions)
	}
}

// Send message to the clinet
// Implements amp.Subscriber interface.
func (s *session) Send(m *amp.Msg) {
	// add to queue
	s.Lock()
	defer s.Unlock()
	s.outQueue = append(s.outQueue, m)
	// signal queue changed
	select {
	case s.outQueueChanged <- struct{}{}:
	default:
	}
	// check for queue overflow
	queueLen := len(s.outQueue)
	if queueLen > maxWriteQueueDepth {
		s.conn.Close()
		s.log().I("len", queueLen).Info("out queue overflow")
	}
	if s.stats.maxQueueLen < queueLen {
		s.stats.maxQueueLen = queueLen
	}
}

func (s *session) connWrite(m *amp.Msg) {
	var payload []byte
	deflated := false
	if s.conn.DeflateSupported() {
		payload, deflated = m.MarshalDeflate()
	} else {
		payload = m.Marshal()
	}
	err := s.conn.Write(payload, deflated)
	if err != nil {
		s.conn.Close()
	}
}

func (s *session) log() *log.Agregator {
	return log.I("no", int(s.conn.No()))
}
