package sharingnode

import "C"
import (
	"bufio"
	"encoding/json"
	"fyne.io/fyne"
	"github.com/go-gl/glfw/v3.2/glfw"
	"github.com/go-vgo/robotgo"
	"math"
	"sync"
	"time"
)

type EventType uint8

const (
	MouseMove EventType = iota
	MouseDrag
	MouseUp
	MouseDown
	KeyUp
	KeyDown
	KeyRepeat
	Scroll
)

type Event struct {
	Type EventType `json:"id"`

	Key    glfw.Key         `json:"keycode"`
	Button glfw.MouseButton `json:"button"`

	X int `json:"x"`
	Y int `json:"y"`

	Xoff float64 `json:"xoff"`
	Yoff float64 `json:"yoff"`
}

type EventSender struct {
	sync.Mutex
	writer           *bufio.Writer
	activeMouseClick bool
	mousePos         fyne.Position
	remoteWidth      int
	remoteHeight     int
	localWidth       int
	localHeight      int
	queue            events
}

func NewEventSender(writer *bufio.Writer, remoteWidth, remoteHeight int) *EventSender {
	return &EventSender{
		writer:       writer,
		remoteWidth:  remoteWidth,
		remoteHeight: remoteHeight,
	}
}

func (e *EventSender) sendEvent(ev *Event) {
	e.Lock()
	defer e.Unlock()
	scaleX := float64(e.remoteWidth) / float64(e.localWidth)
	scaleY := float64(e.remoteHeight) / float64(e.localHeight)

	ev.X = int(float64(ev.X) * scaleX)
	ev.Y = int(float64(ev.Y) * scaleY)

	active := len(e.queue) > 0

	e.queue = append(e.queue, ev)

	if !active {
		go func() {
			time.Sleep(20 * time.Millisecond)

			e.Lock()
			temp := e.queue
			e.queue = events{}
			e.Unlock()

			b, err := json.Marshal(temp)
			if err != nil {
				logger.Error(err)
				return
			}

			b = append(b, '\n')

			_, err = e.writer.Write(b)
			if err != nil {
				logger.Error(err)
				return
			}

			err = e.writer.Flush()
			if err != nil {
				logger.Error(err)
				return
			}
		}()
	}
}

func (e *EventSender) keyEvent(w *glfw.Window, key glfw.Key, scancode int, action glfw.Action, mods glfw.ModifierKey) {
	event := &Event{}
	event.Key = key

	switch action {
	case glfw.Press:
		event.Type = KeyDown
	case glfw.Release:
		event.Type = KeyUp
	case glfw.Repeat:
		event.Type = KeyRepeat
	}

	e.sendEvent(event)
}

func (e *EventSender) mouseMoveEvent(w *glfw.Window, xpos float64, ypos float64) {
	e.mousePos.X = int(xpos)
	e.mousePos.Y = int(ypos)

	event := &Event{}
	event.X = e.mousePos.X
	event.Y = e.mousePos.Y

	switch e.activeMouseClick {
	case true:
		event.Type = MouseDrag
	case false:
		event.Type = MouseMove
	}

	e.sendEvent(event)
}

func (e *EventSender) mouseClick(w *glfw.Window, button glfw.MouseButton, action glfw.Action, mod glfw.ModifierKey) {
	event := &Event{}
	event.X = e.mousePos.X
	event.Y = e.mousePos.Y
	event.Button = button

	switch action {
	case glfw.Press:
		event.Type = MouseDown
		e.activeMouseClick = true
	case glfw.Release:
		event.Type = MouseUp
		e.activeMouseClick = false
	}

	e.sendEvent(event)
}

func (e *EventSender) scrollEvent(w *glfw.Window, xoff float64, yoff float64) {
	event := &Event{}
	event.Xoff = xoff
	event.Yoff = yoff
	event.Type = Scroll

	e.sendEvent(event)
}

func (e *EventSender) Subscribe(win fyne.Window) {
	win.Viewport().SetCursorPosCallback(e.mouseMoveEvent)
	win.Viewport().SetMouseButtonCallback(e.mouseClick)
	win.Viewport().SetScrollCallback(e.scrollEvent)
	win.Viewport().SetKeyCallback(e.keyEvent)
	var superSizeCallback glfw.SizeCallback
	superSizeCallback = win.Viewport().SetSizeCallback(func(w *glfw.Window, width int, height int) {
		e.Lock()
		defer e.Unlock()
		superSizeCallback(w, width, height)
		e.localWidth = width
		e.localHeight = height
	})
}

type EventReceiver struct {
	sync.Mutex
	reader  *bufio.Reader
	offsetX int
}

func NewEventReceiver(reader *bufio.Reader, offsetX int) *EventReceiver {
	return &EventReceiver{
		reader:  reader,
		offsetX: offsetX,
	}
}

type events []*Event

func (e *EventReceiver) receiveEvent() (events, error) {
	b, err := e.reader.ReadBytes('\n')
	if err != nil {
		return nil, err
	}

	ev := &events{}

	err = json.Unmarshal(b, ev)
	if err != nil {
		return nil, err
	}

	for _, event := range *ev {
		event.X = event.X + int(e.offsetX)
	}

	return *ev, nil
}

func (e *EventReceiver) Run() {
	robotgo.SetMouseDelay(0)
	robotgo.SetKeyboardDelay(0)
	robotgo.SetKeyDelay(0)
	for {
		evs, err := e.receiveEvent()
		if err != nil {
			logger.Error(err)
			return
		}

		now := time.Now()
		for _, ev := range evs {
			switch ev.Type {
			case MouseMove:
				robotgo.MoveMouse(int(ev.X), int(ev.Y))
			case MouseDown:
				robotgo.MouseToggle("down", MouseMap[ev.Button])
			case MouseUp:
				robotgo.MouseToggle("up", MouseMap[ev.Button])
			case MouseDrag:
				robotgo.MoveMouse(int(ev.X), int(ev.Y))
			case Scroll:
				direction := "up"
				if ev.Yoff < 0 {
					direction = "down"
				}
				robotgo.ScrollMouse(int(math.Abs(ev.Yoff)*float64(2)), direction)
			case KeyDown:
				key := KeyToString[ev.Key]
				robotgo.KeyToggle(key, "down")
			case KeyUp:
				key := KeyToString[ev.Key]
				robotgo.KeyToggle(key, "up")
			case KeyRepeat:
			default:
				continue
			}
		}
		logger.Debug("Event processed for ", time.Now().Sub(now))
	}
}
