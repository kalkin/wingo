package xclient

import (
	"fmt"
	"github.com/BurntSushi/wingo/render"

	"github.com/BurntSushi/xgb/xproto"

	"github.com/BurntSushi/xgbutil/icccm"
	"github.com/BurntSushi/xgbutil/xevent"
	"github.com/BurntSushi/xgbutil/xprop"
	"github.com/BurntSushi/xgbutil/xrect"
	"github.com/BurntSushi/xgbutil/xwindow"

	"github.com/BurntSushi/wingo/event"
	"github.com/BurntSushi/wingo/frame"
	"github.com/BurntSushi/wingo/hook"
	"github.com/BurntSushi/wingo/logger"
	"github.com/BurntSushi/wingo/stack"
	"github.com/BurntSushi/wingo/wm"
	"github.com/BurntSushi/wingo/workspace"
)

const (
	TypeNormal = iota
	TypeDesktop
	TypeDock
)

var allowedActions = []string{
	"_NET_WM_ACTION_MOVE", "_NET_WM_ACTION_RESIZE",
	"_NET_WM_ACTION_MINIMIZE", "_NET_WM_ACTION_STICK",
	"_NET_WM_ACTION_MAXMIZE_HORZ", "_NET_WM_ACTION_MAXIMIZE_VERT",
	"_NET_WM_ACTION_FULLSCREEN", "_NET_WM_ACTION_CHANGE_DESKTOP",
	"_NET_WM_ACTION_CLOSE", "_NET_WM_ACTION_ABOVE", "_NET_WM_ACTION_BELOW",
}

type Client struct {
	win       *xwindow.Window
	frame     frame.Frame
	workspace workspace.Workspacer

	frames  clientFrames
	states  map[string]clientState
	prompts clientPrompts

	qubesVmName string
	labelColor  int
	name        string
	state       int // One of frame.Active or frame.Inactive.
	layer       int // From constants in stack package.
	maximized   bool
	fullscreen  bool
	iconified   bool
	sticky      bool // Belongs to no workspace.
	skipTaskbar bool
	skipPager   bool

	gtkMaximizeNada bool // When maximized, we should have a nada frame.

	primaryType  int // one of Type[...]
	winTypes     []string
	winStates    []string
	hints        *icccm.Hints
	nhints       *icccm.NormalHints
	protocols    []string
	class        *icccm.WmClass
	transientFor *Client
	time         xproto.Timestamp

	// unmapIgnore is the number of UnmapNotify events to ignore.
	// When 0, an UnmapNotify event causes a client to be unmanaged.
	unmapIgnore int

	// floating, when true, this client will *always* be in the floating layer.
	floating         bool
	moving, resizing bool

	dragGeom  xrect.Rect
	hadStruts bool
	shaped    bool

	attnQuit  chan struct{}
	demanding bool
}

func (c *Client) Map() {
	if c.IsMapped() {
		return
	}
	c.win.Map()
	c.frame.Map()
	icccm.WmStateSet(wm.X, c.Id(), &icccm.WmState{State: icccm.StateNormal})

	event.Notify(event.MappedClient{c.Id()})
}

func (c *Client) Unmap() {
	if !c.IsMapped() {
		return
	}
	c.unmapIgnore++
	c.frame.Unmap()
	c.win.Unmap()
	icccm.WmStateSet(wm.X, c.Id(), &icccm.WmState{State: icccm.StateIconic})

	event.Notify(event.UnmappedClient{c.Id()})
}

func (c *Client) Close() {
	if strIndex("WM_DELETE_WINDOW", c.protocols) > -1 {
		wm_protocols, err := xprop.Atm(wm.X, "WM_PROTOCOLS")
		if err != nil {
			logger.Warning.Println(err)
			return
		}

		wm_del_win, err := xprop.Atm(wm.X, "WM_DELETE_WINDOW")
		if err != nil {
			logger.Warning.Println(err)
			return
		}

		cm, err := xevent.NewClientMessage(32, c.Id(), wm_protocols,
			int(wm_del_win))
		if err != nil {
			logger.Warning.Println(err)
			return
		}

		err = xproto.SendEventChecked(wm.X.Conn(), false, c.Id(), 0,
			string(cm.Bytes())).Check()
		if err != nil {
			logger.Message.Printf("Could not send WM_DELETE_WINDOW "+
				"ClientMessage because: %s", err)
		}
	} else {
		c.win.Kill() // HULK SMASH!
	}
}

func (c *Client) String() string {
	return c.name
}

func (c *Client) Id() xproto.Window {
	return c.win.Id
}

func (c *Client) Win() *xwindow.Window {
	return c.win
}

func (c *Client) TopWin() *xwindow.Window {
	return c.frame.Parent().Window
}

func (c *Client) FireHook(hk hook.Type) {
	args := hook.Args{
		Client: fmt.Sprintf("%d", c.Id()),
	}
	hook.Fire(hk, args)
}

func (c *Client) Layer() int {
	return c.layer
}

func (c *Client) Name() string {
	return c.String()
}

func (c *Client) LabelColor() render.Color {
	switch {
	case c.labelColor == 1:
		return render.NewColor(0xe51c23) // red
	case c.labelColor == 2:
		return render.NewColor(0xff9800) // orange
	case c.labelColor == 3:
		return render.NewColor(0xffeb3b) // yellow
	case c.labelColor == 4:
		return render.NewColor(0x259b24) // green
	case c.labelColor == 5:
		return render.NewColor(0x9e9e9e) // grey
	case c.labelColor == 6:
		return render.NewColor(0x3f51b5) // blue
	case c.labelColor == 7:
		return render.NewColor(0x673ab7) // purple
	case c.labelColor == 8:
		return render.NewColor(0x000000) //black
	}
	return render.NewColor(0x795548)
}

func (c *Client) BackgroundLabelColor() render.Color {
	switch {
	case c.labelColor == 1:
		return render.NewColor(0xf69988) // red
	case c.labelColor == 2:
		return render.NewColor(0xffcc80) // orange
	case c.labelColor == 3:
		return render.NewColor(0xfff59c) // yellow
	case c.labelColor == 4:
		return render.NewColor(0x72d572) // green
	case c.labelColor == 5:
		return render.NewColor(0xeeeeee) // grey
	case c.labelColor == 6:
		return render.NewColor(0x9fa8da) // blue
	case c.labelColor == 7:
		return render.NewColor(0xb39ddb) // purple
	case c.labelColor == 8:
		return render.NewColor(0xffffff) //black
	}
	return render.NewColor(0xbcaaa4)
}

func (c *Client) State() int {
	return c.state
}

func (c *Client) Class() *icccm.WmClass {
	return c.class
}

func (c *Client) Raise() {
	stack.Raise(c)
}
