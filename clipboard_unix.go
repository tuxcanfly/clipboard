// Copyright 2013 @atotto. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build freebsd linux netbsd openbsd solaris dragonfly

package clipboard

import (
	"errors"
	"fmt"
	"log"
	"os/exec"
	"time"

	"github.com/BurntSushi/xgb/xproto"
	"github.com/BurntSushi/xgbutil"
	"github.com/BurntSushi/xgbutil/xevent"
	"github.com/BurntSushi/xgbutil/xwindow"
)

const (
	xsel  = "xsel"
	xclip = "xclip"
)

var (
	Primary bool

	pasteCmdArgs []string
	copyCmdArgs  []string

	xselPasteArgs = []string{xsel, "--output", "--clipboard"}
	xselCopyArgs  = []string{xsel, "--input", "--clipboard"}

	xclipPasteArgs = []string{xclip, "-out", "-selection", "clipboard"}
	xclipCopyArgs  = []string{xclip, "-in", "-selection", "clipboard"}

	missingCommands = errors.New("No clipboard utilities available. Please install xsel or xclip.")

	workTime = 50 * time.Millisecond
)

func init() {
	pasteCmdArgs = xclipPasteArgs
	copyCmdArgs = xclipCopyArgs

	if _, err := exec.LookPath(xclip); err == nil {
		return
	}

	pasteCmdArgs = xselPasteArgs
	copyCmdArgs = xselCopyArgs

	if _, err := exec.LookPath(xsel); err == nil {
		return
	}

	Unsupported = true
}

func getPasteCommand() *exec.Cmd {
	if Primary {
		pasteCmdArgs = pasteCmdArgs[:1]
	}
	return exec.Command(pasteCmdArgs[0], pasteCmdArgs[1:]...)
}

func getCopyCommand() *exec.Cmd {
	if Primary {
		copyCmdArgs = copyCmdArgs[:1]
	}
	return exec.Command(copyCmdArgs[0], copyCmdArgs[1:]...)
}

func readAll() (string, error) {
	if Unsupported {
		return "", missingCommands
	}
	pasteCmd := getPasteCommand()
	out, err := pasteCmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func writeAll(text string) error {
	if Unsupported {
		return missingCommands
	}
	copyCmd := getCopyCommand()
	in, err := copyCmd.StdinPipe()
	if err != nil {
		return err
	}

	if err := copyCmd.Start(); err != nil {
		return err
	}
	if _, err := in.Write([]byte(text)); err != nil {
		return err
	}
	if err := in.Close(); err != nil {
		return err
	}
	return copyCmd.Wait()
}

func compressPropertyNotify(X *xgbutil.XUtil,
	ev xevent.PropertyNotifyEvent) xevent.PropertyNotifyEvent {

	// We force a round trip request so that we make sure to read all
	// available events.
	X.Sync()
	xevent.Read(X, false)

	// The most recent PropertyNotify event that we'll end up returning.
	laste := ev

	// Look through each event in the queue. If it's an event and it matches
	// all the fields in 'ev' that are detailed above, then set it to 'laste'.
	// In which case, we'll also dequeue the event, otherwise it will be
	// processed twice!
	// N.B. If our only goal was to find the most recent relevant PropertyNotify
	// event, we could traverse the event queue backwards and simply use
	// the first PropertyNotify we see. However, this could potentially leave
	// other PropertyNotify events in the queue, which we *don't* want to be
	// processed. So we stride along and just pick off PropertyNotify events
	// until we don't see any more.
	for i, ee := range xevent.Peek(X) {
		if ee.Err != nil { // This is an error, skip it.
			continue
		}

		// Use type assertion to make sure this is a PropertyNotify event.
		if mn, ok := ee.Event.(xproto.PropertyNotifyEvent); ok {
			// Now make sure all appropriate fields are equivalent.
			if ev.Atom == mn.Atom && ev.Window == mn.Window {

				// Set the most recent/valid motion notify event.
				laste = xevent.PropertyNotifyEvent{&mn}

				// We cheat and use the stack semantics of defer to dequeue
				// most recent motion notify events first, so that the indices
				// don't become invalid. (If we dequeued oldest first, we'd
				// have to account for all future events shifting to the left
				// by one.)
				defer func(i int) { xevent.DequeueAt(X, i) }(i)
			}
		}
	}

	// This isn't strictly necessary, but is correct. We should update
	// xgbutil's sense of time with the most recent event processed.
	// This is typically done in the main event loop, but since we are
	// subverting the main event loop, we should take care of it.
	X.TimeSet(laste.Time)

	return laste
}

func monitorAll(text chan<- string, quit <-chan struct{}) error {
	X, err := xgbutil.NewConn()
	if err != nil {
		log.Fatal(err)
	}

	xwindow.New(X, X.RootWin()).Listen(xproto.EventMaskPropertyChange)

	atomCookie := xproto.InternAtom(X.Conn(), true, 14, "CLIP_TEMPORARY")
	var atomReply *xproto.InternAtomReply
	atomReply, err = atomCookie.Reply()
	if err != nil {
		return err
	}

	// Respond to those X events.
	xevent.PropertyNotifyFun(
		func(X *xgbutil.XUtil, ev xevent.PropertyNotifyEvent) {
			if ev.Atom != atomReply.Atom {
				return
			}
			if ev.State != 1 {
				return
			}
			ev = compressPropertyNotify(X, ev)
			fmt.Println(ev)
			time.Sleep(workTime)
		}).Connect(X, X.RootWin())

	xevent.Main(X)
	return nil
}
