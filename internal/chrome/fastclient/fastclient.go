// fastclient provides a faster implementation of rod.CDPClient

package fastclient

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/bytedance/sonic"
	"github.com/go-rod/rod/lib/cdp"
)

type Request struct {
	ID        int    `json:"id"`
	SessionID string `json:"sessionId,omitempty"`
	Method    string `json:"method"`
	Params    any    `json:"params,omitempty"`
}

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data"`
}

type Response struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}

type ResponseResult struct {
	done chan struct{}
	set  bool
	buff []byte
	err  *Error
}

// Client is a faster implementation of rod.CDPClient
type Client struct {
	ws     cdp.WebSocketable
	events chan *cdp.Event
	count  int

	freed     []int
	responses []ResponseResult
	mu        sync.Mutex
}

func NewClient() *Client {
	return &Client{
		events: make(chan *cdp.Event, 4),
	}
}

func (c *Client) messageWorker() {
	defer sync.OnceFunc(c.closeAll)()

	for {
		data, err := c.ws.Read()
		if err != nil {
			panic(err)
		}

		idNode, err := sonic.Get(data, "id")
		if err != nil {
			panic(err)
		}
		id, err := idNode.Int64()
		if err != nil {
			panic(err)
		}

		if id == 0 {
			var event cdp.Event
			err = sonic.ConfigFastest.Unmarshal(data, &event)
			if err != nil {
				panic(err)
			}
			c.events <- &event
			continue
		}

		// always subtract by one since the id is always added by one
		id--

		var res Response
		err = sonic.ConfigFastest.Unmarshal(data, &res)
		if err != nil {
			panic(err)
		}
		c.responses[id].set = true
		c.responses[id].buff = res.Result
		c.responses[id].err = res.Error
		c.responses[id].done <- struct{}{}
	}
}

func (c *Client) Call(ctx context.Context, sessionID, method string, params any) (buff []byte, err error) {
	id, done := c.addPending()
	defer c.freePending(id)

	req := Request{
		// always add by one because id == 0 indicates an event
		ID:        id + 1,
		SessionID: sessionID,
		Method:    method,
		Params:    params,
	}

	data, err := sonic.ConfigFastest.Marshal(req)
	if err != nil {
		panic(err)
	}

	err = c.ws.Send(data)
	if err != nil {
		return
	}
	<-done

	res := c.responses[id]
	if !res.set {
		err = fmt.Errorf("cdp call: canceled")
		return
	}
	buff = res.buff
	err = res.err.Error()
	return
}

func (c *Client) addPending() (id int, done chan struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.freed) > 0 {
		id = c.freed[len(c.freed)-1]
		c.freed = c.freed[:len(c.freed)-1]

		c.responses[id].set = false
		c.responses[id].buff = nil
		c.responses[id].err = nil

		done = c.responses[id].done
		return
	}

	id = c.count
	done = make(chan struct{})
	c.responses = append(c.responses, ResponseResult{
		done: done,
	})
	c.count++
	return
}

func (c *Client) freePending(id int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.freed = append(c.freed, id)
}

func (c *Client) closeAll() {
	// c.events may be already closed
	_, open := <-c.events
	if open {
		close(c.events)
	}
	for _, r := range c.responses {
		close(r.done)
	}
}

func (c *Client) Start(ws cdp.WebSocketable, workers int) {
	c.ws = ws
	for range workers {
		go c.messageWorker()
	}
}

func (c *Client) Event() <-chan *cdp.Event {
	return c.events
}

func (e *Error) Error() error {
	if e == nil {
		return nil
	}
	return fmt.Errorf("cdp call (%d): %s (data: %s)", e.Code, e.Message, e.Data)
}
