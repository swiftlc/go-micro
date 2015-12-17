package client

import (
	"errors"
	"io"
	"log"
	"sync"

	"golang.org/x/net/context"
)

type rpcStream struct {
	sync.RWMutex
	seq     uint64
	closed  bool
	err     error
	request Request
	codec   clientCodec
	context context.Context
}

func (r *rpcStream) Context() context.Context {
	return r.context
}

func (r *rpcStream) Request() Request {
	return r.request
}

func (r *rpcStream) Send(msg interface{}) error {
	r.Lock()
	defer r.Unlock()

	if r.closed {
		r.err = errShutdown
		return errShutdown
	}

	seq := r.seq
	r.seq++

	req := request{
		Service:       r.request.Service(),
		Seq:           seq,
		ServiceMethod: r.request.Method(),
	}

	if err := r.codec.WriteRequest(&req, msg); err != nil {
		r.err = err
		return err
	}

	return nil
}

func (r *rpcStream) Recv(msg interface{}) error {
	r.Lock()
	defer r.Unlock()

	if r.closed {
		r.err = errShutdown
		return errShutdown
	}

	var resp response
	if err := r.codec.ReadResponseHeader(&resp); err != nil {
		if err == io.EOF && !r.closed {
			r.err = io.ErrUnexpectedEOF
			return io.ErrUnexpectedEOF
		}
		r.err = err
		return err
	}

	switch {
	case len(resp.Error) > 0:
		// We've got an error response. Give this to the request;
		// any subsequent requests will get the ReadResponseBody
		// error if there is one.
		if resp.Error != lastStreamResponseError {
			r.err = serverError(resp.Error)
		} else {
			r.err = io.EOF
		}
		if err := r.codec.ReadResponseBody(nil); err != nil {
			r.err = errors.New("reading error payload: " + err.Error())
		}
	default:
		if err := r.codec.ReadResponseBody(msg); err != nil {
			r.err = errors.New("reading body " + err.Error())
		}
	}

	if r.err != nil && r.err != io.EOF && !r.closed {
		log.Println("rpc: client protocol error:", r.err)
	}

	return r.err
}

func (r *rpcStream) Error() error {
	r.RLock()
	defer r.RUnlock()
	return r.err
}

func (r *rpcStream) Close() error {
	r.Lock()
	defer r.Unlock()
	r.closed = true
	return r.codec.Close()
}
