package router

import (
	"container/list"
	"encoding/json"
	"net"
	"sync"
	"time"

	"github.com/wandoulabs/codis/pkg/proxy/redis"
	"github.com/wandoulabs/codis/pkg/utils/atomic2"
	"github.com/wandoulabs/codis/pkg/utils/errors"
	"github.com/wandoulabs/codis/pkg/utils/log"
)

type Session struct {
	*redis.Conn

	Sid    int64
	SeqId  int64
	Closed bool

	CreateUnix int64
}

func (s *Session) String() string {
	o := &struct {
		Sid        int64
		SeqId      int64
		CreateUnix int64
		RemoteAddr string
	}{
		s.Sid, s.SeqId, s.CreateUnix,
		s.Conn.Sock.RemoteAddr().String(),
	}
	b, _ := json.Marshal(o)
	return string(b)
}

func NewSession(c net.Conn) *Session {
	s := &Session{Sid: sessions.sid.Incr(), CreateUnix: time.Now().Unix()}
	s.Conn = redis.NewConn(c)
	s.Conn.ReaderTimeout = time.Minute * 30
	s.Conn.WriterTimeout = time.Minute
	go s.Run()
	return addNewSession(s)
}

func (s *Session) Close() {
	s.Closed = true
	s.Conn.Close()
}

func (s *Session) Run() {
	var errlist errors.ErrorList
	defer func() {
		log.Infof("session [%p] closed, session = %s, error = %s", s, s, errlist.First())
	}()

	tasks := make(chan *Request, 256)
	go func() {
		if err := s.loopWriter(tasks); err != nil {
			errlist.PushBack(err)
		}
		s.Close()
		for _ = range tasks {
		}
	}()

	defer close(tasks)
	for {
		resp, err := s.Reader.Decode()
		if err != nil {
			errlist.PushBack(err)
			return
		}
		tasks <- s.handleRequest(resp)
	}
}

func (s *Session) loopWriter(tasks <-chan *Request) error {
	for r := range tasks {
		resp, err := s.handleResponse(r)
		if resp != nil {
			err = s.Writer.Encode(resp, r.Flush || len(tasks) == 0)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Session) handleRequest(resp *redis.Resp) *Request {
	s.SeqId++
	r := &Request{
		Sid: s.Sid, SeqId: s.SeqId,
	}
	panic("todo")
	return r
	// if success then r.wait.Add(1)
}

func (s *Session) handleResponse(r *Request) (*redis.Resp, error) {
	r.Wait()
	panic("todo")
}

var sessions struct {
	sid atomic2.Int64
	list.List
	sync.Mutex
}

func init() {
	go func() {
		for {
			time.Sleep(time.Minute)
			lastunix := time.Now().Add(-time.Minute * 45).Unix()
			cleanupSessions(lastunix)
		}
	}()
}

func addNewSession(s *Session) *Session {
	sessions.Lock()
	sessions.PushBack(s)
	sessions.Unlock()
	log.Infof("session [%p] created, sid = %d", s, s.Sid)
	return s
}

func cleanupSessions(lastunix int64) {
	sessions.Lock()
	for i := sessions.Len(); i != 0; i-- {
		e := sessions.Front()
		s := e.Value.(*Session)
		if s.Closed {
			sessions.Remove(e)
		} else if s.IsTimeout(lastunix) {
			log.Infof("session [%p] killed, due to timeout, sid = %d", s, s.Sid)
			s.Close()
			sessions.Remove(e)
		} else {
			sessions.MoveToBack(e)
		}
	}
	sessions.Unlock()
}