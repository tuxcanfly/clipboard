// Copyright 2013 @atotto. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build freebsd linux netbsd openbsd solaris dragonfly

package clipboard

import (
	"errors"
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

	events := make(chan xevent.PropertyNotifyEvent)
	go func() {
		var last uint32
		var old string
		for {
			select {
			case ev := <-events:
				if uint32(ev.Time)-last > 100 {
					copy, err := readAll()
					if err != nil {
						continue
					}
					if copy != old {
						text <- copy
						old = copy
					}
				}
				last = uint32(ev.Time)
			case <-quit:
				return
			}
		}
	}()

	// Respond to those X events.
	xevent.PropertyNotifyFun(
		func(X *xgbutil.XUtil, ev xevent.PropertyNotifyEvent) {
			if ev.Atom != atomReply.Atom || ev.State != 1 {
				return
			}
			events <- ev
		}).Connect(X, X.RootWin())

	xevent.Main(X)
	return nil
}
