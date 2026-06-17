package main

import (
	"ax-distiller/internal/structure"
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/LQR471814/rod"
	"github.com/LQR471814/rod/lib/input"
)

type Location string

type Search struct {
	From   Location
	To     Location
	Depart time.Time
	Return *time.Time
}

type Controller struct {
	logger   *slog.Logger
	page     *rod.Page
	persist  *structure.Persistent
	events   chan Event
	actions  chan Action
	searches []Search
	onDone   chan error

	state     SiteState
	searchIdx int
	setFrom   bool
	setTo     bool
	setDates  bool
	done      bool
	err       error
}

func NewController(
	logger *slog.Logger,
	page *rod.Page,
	persist *structure.Persistent,
	searches []Search,
	onDone chan error,
) *Controller {
	return &Controller{
		logger:   logger,
		page:     page,
		persist:  persist,
		events:   make(chan Event),
		actions:  make(chan Action),
		searches: searches,
		onDone:   onDone,

		state:     site_state_home,
		searchIdx: 0,
		setFrom:   false,
		setTo:     false,
		setDates:  false,
		done:      false,
		err:       nil,
	}
}

func (c *Controller) actionProcessor(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case action, ok := <-c.actions:
			if !ok {
				return
			}
			c.logger.Info("do action", "value", fmt.Sprintf("%T", action))
			ev := action(c.page)
			if ev != nil {
				c.DispatchEvent(ev)
			}
			if c.done {
				return
			}
		}
	}
}

func (c *Controller) eventLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-c.events:
			if !ok {
				return
			}
			c.logger.Info("get event", "value", fmt.Sprintf("%T", event))
			c.processEvent(event)
		}
	}
}

func (c *Controller) StartWorkers(ctx context.Context) {
	go c.actionProcessor(ctx)
	go c.eventLoop(ctx)
}

func (c *Controller) cleanup() {
	c.done = true
	c.onDone <- c.err
	close(c.actions)
}

func (c *Controller) nextSearch() bool {
	if c.searchIdx >= len(c.searches) {
		c.cleanup()
		return false
	}
	c.searchIdx++
	return true
}

func (c *Controller) currentSearch() Search {
	return c.searches[c.searchIdx]
}

type ControllerTreeUpdate struct{}
type fromClickDone struct{}
type toClickDone struct{}
type dateClickDone struct{}
type locInputDone struct{}
type dateInputDone struct{}

func (ControllerTreeUpdate) Event() {}
func (fromClickDone) Event()        {}
func (toClickDone) Event()          {}
func (dateClickDone) Event()        {}
func (locInputDone) Event()         {}
func (dateInputDone) Event()        {}

func (c *Controller) setState(state SiteState) {
	c.state = state
}

func (c *Controller) dispatchAction(action Action) {
	c.actions <- action
}

type SiteState int

const (
	site_state_home SiteState = iota
	site_state_location_picker_active
	site_state_date_picker_active
)

func (c *Controller) DispatchEvent(ev Event) {
	c.events <- ev
}

func (c *Controller) processEvent(ev Event) {
	if c.done {
		return
	}
	switch ev := ev.(type) {
	case ErrEvent:
		c.err = ev.Err
		c.cleanup()
		return
	case ControllerTreeUpdate:
		switch c.state {
		case site_state_home:
			c.handleHomeState()
		case site_state_location_picker_active:
			search := c.currentSearch()
			c.locationInput(string(search.From))
		case site_state_date_picker_active:
			search := c.currentSearch()
			c.locationInput(string(search.To))
		}
	// case fromClickDone:
	// 	switch c.state {
	// 	case site_state_location_picker_active:
	// 	}
	// case toClickDone:
	// 	switch c.state {
	// 	case site_state_location_picker_active:
	// 	}
	// case dateClickDone:
	// 	switch c.state {
	// 	case site_state_date_picker_active:
	// 	}
	case locInputDone:
		c.handleHomeState()
	case dateInputDone:
		c.handleHomeState()
	}
}

func (c *Controller) dateInput() {
	dateInputs := c.persist.InstancesByStructHash(801387602716526053)
	if len(dateInputs) != 2 {
		go c.DispatchEvent(
			ErrEvent{Err: fmt.Errorf("should exist exactly two date inputs (defaults)")},
		)
		return
	}

	departSt := dateInputs[0]
	returnSt := dateInputs[1]

	search := c.currentSearch()

	c.state = site_state_home
	c.dispatchAction(func(page *rod.Page) Event {
		departEl, err := GetElementByBackendID(page, departSt.Underlying.Underlying.BackendDOMNodeID)
		if err != nil {
			return ErrEvent{Err: err}
		}
		err = departEl.Input(search.Depart.Format("2006/1/2"))
		if err != nil {
			return ErrEvent{Err: err}
		}
		page.Keyboard.Type(input.Enter)

		if search.Return != nil {
			returnEl, err := GetElementByBackendID(page, returnSt.Underlying.Underlying.BackendDOMNodeID)
			if err != nil {
				return ErrEvent{Err: err}
			}
			err = returnEl.Input(search.Return.Format("2006/1/2"))
			if err != nil {
				return ErrEvent{Err: err}
			}
			page.Keyboard.Type(input.Enter)
		}

		return dateInputDone{}
	})
}

func (c *Controller) locationInput(term string) {
	textInput := c.persist.InstancesByStructHash(16293937389406049723)
	if len(textInput) != 1 {
		go c.DispatchEvent(ErrEvent{Err: fmt.Errorf("should exist only one text input")})
		return
	}
	fmt.Println(textInput)

	c.setState(site_state_home)
	c.dispatchAction(func(page *rod.Page) Event {
		el, err := GetElementByBackendID(page, textInput[0].Underlying.Underlying.BackendDOMNodeID)
		err = el.Input(term)
		if err != nil {
			return ErrEvent{Err: err}
		}
		err = el.Page().Keyboard.Type(input.Enter)
		if err != nil {
			return ErrEvent{Err: err}
		}
		return locInputDone{}
	})
}

func (c *Controller) handleHomeState() {
	containers := c.persist.InstancesByStructHash(16101408840648308877)
	if len(containers) == 1 {
		fmt.Println(containers)
	} else {
		go c.DispatchEvent(ErrEvent{
			Err: fmt.Errorf("unexpected # of container (%d != 1)", len(containers)),
		})
		return
	}

	// lookup the froms
	froms := c.persist.InstancesByStructHash(7372408943638842523)
	if len(froms) == 1 && !c.setFrom {
		from := froms[0]
		c.setFrom = true
		c.setState(site_state_location_picker_active)
		c.dispatchAction(LeftClickAction(
			from.Underlying.Underlying.BackendDOMNodeID,
			fromClickDone{},
		))
	} else {
		go c.DispatchEvent(ErrEvent{
			Err: fmt.Errorf("unexpected # of from picker (%d != 1)", len(froms)),
		})
		return
	}

	tos := c.persist.InstancesByStructHash(8406647588520333505)
	if len(tos) == 1 && !c.setTo {
		to := tos[0]
		c.setTo = true
		c.setState(site_state_location_picker_active)
		c.dispatchAction(LeftClickAction(
			to.Underlying.Underlying.BackendDOMNodeID,
			toClickDone{},
		))
	} else {
		go c.DispatchEvent(ErrEvent{
			Err: fmt.Errorf("unexpected # of to picker (%d != 1)", len(tos)),
		})
		return
	}

	dates := c.persist.InstancesByStructHash(801387602716526053)
	if len(dates) == 2 && !c.setDates {
		start := dates[0]
		c.setDates = true
		c.dispatchAction(LeftClickAction(
			start.Underlying.Underlying.BackendDOMNodeID,
			dateClickDone{},
		))
	} else {
		go c.DispatchEvent(ErrEvent{
			Err: fmt.Errorf("unexpected # of to dates picker (%d != 2)", len(dates)),
		})
		return
	}
}
