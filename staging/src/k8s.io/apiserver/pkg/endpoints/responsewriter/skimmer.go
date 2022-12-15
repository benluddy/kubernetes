package responsewriter

import (
	"bytes"
	"net"
	"strconv"
	"sync"
)

func Skim(c net.Conn) *Skimmer {
	return &Skimmer{
		Conn:  c,
		limit: 64,
	}
}

type Skimmer struct {
	net.Conn

	limit  int
	buffer bytes.Buffer
	status int
	lock   sync.Mutex
}

func (rs *Skimmer) Status() (int, bool) {
	rs.lock.Lock()
	defer rs.lock.Unlock()

	if rs.status < 0 {
		return 0, false
	}

	return rs.status, true
}

func (rs *Skimmer) Write(p []byte) (int, error) {
	rs.lock.Lock()
	defer rs.lock.Unlock()

	cn, cerr := rs.Conn.Write(p)
	if rs.status != 0 {
		return cn, cerr
	}

	p = p[:cn] // only examine bytes written to conn

	if rs.buffer.Len()+len(p) > rs.limit {
		p = p[:rs.limit-rs.buffer.Len()]
	}

	i := bytes.IndexByte(p, '\n')
	if i < 0 {
		rs.buffer.Write(p)
		if rs.buffer.Len() >= rs.limit {
			rs.status = -1 // forfeit
		}
		return cn, cerr
	}

	rs.buffer.Write(p[:i]) // intentionally discards \n
	line := rs.buffer.Bytes()
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}

	// todo: allocs?
	_, status, ok := bytes.Cut(line, []byte{' '})
	if !ok {
		rs.status = -2
		return cn, cerr
	}
	status = bytes.TrimLeft(status, " ")

	statusCode, _, _ := bytes.Cut(status, []byte{' '})
	if len(statusCode) != 3 {
		rs.status = -3
		return cn, cerr
	}
	var err error
	rs.status, err = strconv.Atoi(string(statusCode))
	if err != nil || rs.status < 0 {
		rs.status = -4
		return cn, cerr
	}

	return cn, cerr
}
