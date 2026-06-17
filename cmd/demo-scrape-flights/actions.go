package main

import (
	"time"

	"github.com/LQR471814/rod"
	"github.com/LQR471814/rod/lib/proto"
)

type Event interface {
	Event()
}

type ErrEvent struct {
	Err error
}

func (ErrEvent) Event() {}

type Action func(page *rod.Page) Event

func DispatchEventAction(event Event) Action {
	return func(page *rod.Page) Event {
		return event
	}
}

func GetElementByBackendID(page *rod.Page, backendID proto.DOMBackendNodeID) (el *rod.Element, err error) {
	time.Sleep(time.Second)
	obj, err := ObjectFromBackendID(page, backendID)
	if err != nil {
		return
	}
	el, err = page.ElementFromObject(obj)
	return
}

func LeftClickAction(backendID proto.DOMBackendNodeID, event Event) Action {
	return func(page *rod.Page) (ev Event) {
		el, err := GetElementByBackendID(page, backendID)
		if err != nil {
			ev = ErrEvent{Err: err}
			return
		}
		err = el.Click(proto.InputMouseButtonLeft, 1)
		if err != nil {
			ev = ErrEvent{Err: err}
			return
		}
		ev = event
		return
	}
}
