// Package tea provides a framework for building rich terminal user interfaces
// based on the paradigms of The Elm Architecture. It's well-suited for simple
// and complex terminal applications, either inline, full-window, or a mix of
// both. It's been battle-tested in several large projects and is
// production-ready.
//
// A tutorial is available at https://github.com/charmbracelet/bubbletea/tree/master/tutorials
//
// Example programs can be found at https://github.com/charmbracelet/bubbletea/tree/master/examples
package tea

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"sync"
	"syscall"

	"github.com/containerd/console"
	isatty "github.com/mattn/go-isatty"
	"github.com/muesli/cancelreader"
	"github.com/muesli/termenv"
	"golang.org/x/term"
)

// ErrProgramKilled is returned by [Program.Run] when the program got killed.
var ErrProgramKilled = errors.New("program was killed")

// Msg contain data from the result of a IO operation. Msgs trigger the update
// function and, henceforth, the UI.
type Msg interface{}

// Model contains the program's state as well as its core functions.
type Model interface {
	// Init is the first function that will be called. It returns an optional
	// initial command. To not perform an initial command return nil.
	Init() Cmd

	// Update is called when a message is received. Use it to inspect messages
	// and, in response, update the model and/or send a command.
	Update(Msg) (Model, Cmd)

	// View renders the program's UI, which is just a string. The view is
	// rendered after every Update.
	View() string
}

// Cmd is an IO operation that returns a message when it's complete. If it's
// nil it's considered a no-op. Use it for things like HTTP requests, timers,
// saving and loading from disk, and so on.
//
// Note that there's almost never a reason to use a command to send a message
// to another part of your program. That can almost always be done in the
// update function.
type Cmd func() Msg

type handlers []chan struct{}

// Options to customize the program during its initialization. These are
// generally set with ProgramOptions.
//
// The options here are treated as bits.
type startupOptions byte

func (s startupOptions) has(option startupOptions) bool {
	return s&option != 0
}

const (
	withAltScreen startupOptions = 1 << iota
	withMouseCellMotion
	withMouseAllMotion
	withInputTTY
	withCustomInput
	withANSICompressor
	withoutSignalHandler

	// Catching panics is incredibly useful for restoring the terminal to a
	// usable state after a panic occurs. When this is set, Bubble Tea will
	// recover from panics, print the stack trace, and disable raw mode. This
	// feature is on by default.
	withoutCatchPanics
)

// Program is a terminal user interface.
type Program struct {
	initialModel Model

	// Configuration options that will set as the program is initializing,
	// treated as bits. These options can be set via various ProgramOptions.
	startupOptions startupOptions

	ctx    context.Context
	cancel context.CancelFunc

	msgs chan Msg
	errs chan error

	// where to send output, this will usually be os.Stdout.
	output        *termenv.Output
	restoreOutput func() error
	renderer      renderer

	// where to read inputs from, this will usually be os.Stdin.
	input        io.Reader
	cancelReader cancelreader.CancelReader
	readLoopDone chan struct{}
	console      console.Console

	// was the altscreen active before releasing the terminal?
	altScreenWasActive bool
	ignoreSignals      bool

	// Stores the original reference to stdin for cases where input is not a
	// TTY on windows and we've automatically opened CONIN$ to receive input.
	// When the program exits this will be restored.
	//
	// Lint ignore note: the linter will find false positive on unix systems
	// as this value only comes into play on Windows, hence the ignore comment
	// below.
	windowsStdin *os.File //nolint:golint,structcheck,unused
}

// Quit is a special command that tells the Bubble Tea program to exit.
func Quit() Msg {
	return quitMsg{}
}

// quitMsg in an internal message signals that the program should quit. You can
// send a quitMsg with Quit.
type quitMsg struct{}

// NewProgram creates a new Program.
func NewProgram(model Model, opts ...ProgramOption) *Program {
	p := &Program{
		initialModel: model,
		input:        os.Stdin,
		msgs:         make(chan Msg),
	}

	// Initialize context and teardown channel.
	p.ctx, p.cancel = context.WithCancel(context.Background())

	// Apply all options to the program.
	for _, opt := range opts {
		opt(p)
	}

	// if no output was set, set it to stdout
	if p.output == nil {
		p.output = termenv.DefaultOutput()

		// cache detected color values
		termenv.WithColorCache(true)(p.output)
	}

	p.restoreOutput, _ = termenv.EnableVirtualTerminalProcessing(p.output)

	return p
}

func (p *Program) handleSignals() chan struct{} {
	ch := make(chan struct{})

	// Listen for SIGINT and SIGTERM.
	//
	// In most cases ^C will not send an interrupt because the terminal will be
	// in raw mode and ^C will be captured as a keystroke and sent along to
	// Program.Update as a KeyMsg. When input is not a TTY, however, ^C will be
	// caught here.
	//
	// SIGTERM is sent by unix utilities (like kill) to terminate a process.
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		defer func() {
			signal.Stop(sig)
			close(ch)
		}()

		for {
			select {
			case <-p.ctx.Done():
				return

			case <-sig:
				if !p.ignoreSignals {
					p.msgs <- quitMsg{}
					return
				}
			}
		}
	}()

	return ch
}

// handleResize handles terminal resize events.
func (p *Program) handleResize() chan struct{} {
	ch := make(chan struct{})

	if f, ok := p.output.TTY().(*os.File); ok && isatty.IsTerminal(f.Fd()) {
		// Get the initial terminal size and send it to the program.
		go func() {
			w, h, err := term.GetSize(int(f.Fd()))
			if err != nil {
				p.errs <- err
			}

			select {
			case <-p.ctx.Done():
			case p.msgs <- WindowSizeMsg{w, h}:
			}
		}()

		// Listen for window resizes.
		go listenForResize(p.ctx, f, p.msgs, p.errs, ch)
	} else {
		close(ch)
	}

	return ch
}

// handleCommands runs commands in a goroutine and sends the result to the
// program's message channel.
func (p *Program) handleCommands(cmds chan Cmd) chan struct{} {
	ch := make(chan struct{})

	go func() {
		defer close(ch)

		for {
			select {
			case <-p.ctx.Done():
				return

			case cmd := <-cmds:
				if cmd == nil {
					continue
				}

				// Don't wait on these goroutines, otherwise the shutdown
				// latency would get too large as a Cmd can run for some time
				// (e.g. tick commands that sleep for half a second). It's not
				// possible to cancel them so we'll have to leak the goroutine
				// until Cmd returns.
				go p.Send(cmd())
			}
		}
	}()

	return ch
}

// eventLoop is the central message loop. It receives and handles the default
// Bubble Tea messages, update the model and triggers redraws.
func (p *Program) eventLoop(model Model, cmds chan Cmd) (Model, error) {
	for {
		select {
		case <-p.ctx.Done():
			return model, nil

		case err := <-p.errs:
			return model, err

		case msg := <-p.msgs:
			// Handle special internal messages.
			switch msg := msg.(type) {
			case quitMsg:
				return model, nil

			case clearScreenMsg:
				p.renderer.clearScreen()

			case enterAltScreenMsg:
				p.renderer.enterAltScreen()

			case exitAltScreenMsg:
				p.renderer.exitAltScreen()

			case enableMouseCellMotionMsg:
				p.renderer.enableMouseCellMotion()

			case enableMouseAllMotionMsg:
				p.renderer.enableMouseAllMotion()

			case disableMouseMsg:
				p.renderer.disableMouseCellMotion()
				p.renderer.disableMouseAllMotion()

			case showCursorMsg:
				p.renderer.showCursor()

			case hideCursorMsg:
				p.renderer.hideCursor()

			case execMsg:
				// NB: this blocks.
				p.exec(msg.cmd, msg.fn)

			case batchMsg:
				for _, cmd := range msg {
					cmds <- cmd
				}
				continue

			case sequenceMsg:
				go func() {
					// Execute commands one at a time, in order.
					for _, cmd := range msg {
						p.Send(cmd())
					}
				}()
			}

			// Process internal messages for the renderer.
			if r, ok := p.renderer.(*standardRenderer); ok {
				r.handleMessages(msg)
			}

			var cmd Cmd
			model, cmd = model.Update(msg) // run update
			cmds <- cmd                    // process command (if any)
			p.renderer.write(model.View()) // send view to renderer
		}
	}
}

// Run initializes the program and runs its event loops, blocking until it gets
// terminated by either [Program.Quit], [Program.Kill], or its signal handler.
// Returns the final model.
func (p *Program) Run() (Model, error) {
	handlers := handlers{}
	cmds := make(chan Cmd)
	p.errs = make(chan error)

	defer p.cancel()

	switch {
	case p.startupOptions.has(withInputTTY):
		// Open a new TTY, by request
		f, err := openInputTTY()
		if err != nil {
			return p.initialModel, err
		}
		p.input = f

	case !p.startupOptions.has(withCustomInput):
		// If the user hasn't set a custom input, and input's not a terminal,
		// open a TTY so we can capture input as normal. This will allow things
		// to "just work" in cases where data was piped or redirected into this
		// application.
		f, isFile := p.input.(*os.File)
		if !isFile {
			break
		}
		if isatty.IsTerminal(f.Fd()) {
			break
		}

		f, err := openInputTTY()
		if err != nil {
			return p.initialModel, err
		}
		p.input = f
	}

	if f, ok := p.input.(io.ReadCloser); ok {
		defer f.Close() //nolint:errcheck
	}

	// Handle signals.
	if !p.startupOptions.has(withoutSignalHandler) {
		handlers.add(p.handleSignals())
	}

	// Recover from panics.
	if !p.startupOptions.has(withoutCatchPanics) {
		defer func() {
			if r := recover(); r != nil {
				p.shutdown(true)
				fmt.Printf("Caught panic:\n\n%s\n\nRestoring terminal...\n\n", r)
				debug.PrintStack()
				return
			}
		}()
	}

	// If no renderer is set use the standard one.
	if p.renderer == nil {
		p.renderer = newRenderer(p.output, p.startupOptions.has(withANSICompressor))
	}

	// Check if output is a TTY before entering raw mode, hiding the cursor and
	// so on.
	if err := p.initTerminal(); err != nil {
		return p.initialModel, err
	}

	// Honor program startup options.
	if p.startupOptions&withAltScreen != 0 {
		p.renderer.enterAltScreen()
	}
	if p.startupOptions&withMouseCellMotion != 0 {
		p.renderer.enableMouseCellMotion()
	} else if p.startupOptions&withMouseAllMotion != 0 {
		p.renderer.enableMouseAllMotion()
	}

	// Initialize the program.
	model := p.initialModel
	if initCmd := model.Init(); initCmd != nil {
		ch := make(chan struct{})
		handlers.add(ch)

		go func() {
			defer close(ch)

			select {
			case cmds <- initCmd:
			case <-p.ctx.Done():
			}
		}()
	}

	// Start the renderer.
	p.renderer.start()

	// Render the initial view.
	p.renderer.write(model.View())

	// Subscribe to user input.
	if p.input != nil {
		if err := p.initCancelReader(); err != nil {
			return model, err
		}
		defer p.cancelReader.Close() //nolint:errcheck
	}

	// Handle resize events.
	handlers.add(p.handleResize())

	// Process commands.
	handlers.add(p.handleCommands(cmds))

	// Run event loop, handle updates and draw.
	model, err := p.eventLoop(model, cmds)
	killed := p.ctx.Err() != nil
	if killed {
		err = ErrProgramKilled
	} else {
		// Ensure we rendered the final state of the model.
		p.renderer.write(model.View())
	}

	// Tear down.
	p.cancel()

	// Wait for input loop to finish.
	if p.cancelReader.Cancel() {
		p.waitForReadLoop()
	}

	// Wait for all handlers to finish.
	handlers.shutdown()

	// Restore terminal state.
	p.shutdown(killed)

	return model, err
}

// StartReturningModel initializes the program and runs its event loops,
// blocking until it gets terminated by either [Program.Quit], [Program.Kill],
// or its signal handler. Returns the final model.
//
// Deprecated: please use [Program.Run] instead.
func (p *Program) StartReturningModel() (Model, error) {
	return p.Run()
}

// Start initializes the program and runs its event loops, blocking until it
// gets terminated by either [Program.Quit], [Program.Kill], or its signal
// handler.
//
// Deprecated: please use [Program.Run] instead.
func (p *Program) Start() error {
	_, err := p.Run()
	return err
}

// Send sends a message to the main update function, effectively allowing
// messages to be injected from outside the program for interoperability
// purposes.
//
// If the program hasn't started yet this will be a blocking operation.
// If the program has already been terminated this will be a no-op, so it's safe
// to send messages after the program has exited.
func (p *Program) Send(msg Msg) {
	select {
	case <-p.ctx.Done():
	case p.msgs <- msg:
	}
}

// Quit is a convenience function for quitting Bubble Tea programs. Use it
// when you need to shut down a Bubble Tea program from the outside.
//
// If you wish to quit from within a Bubble Tea program use the Quit command.
//
// If the program is not running this will be a no-op, so it's safe to call
// if the program is unstarted or has already exited.
func (p *Program) Quit() {
	p.Send(Quit())
}

// Kill stops the program immediately and restores the former terminal state.
// The final render that you would normally see when quitting will be skipped.
// [program.Run] returns a [ErrProgramKilled] error.
func (p *Program) Kill() {
	p.cancel()
}

// shutdown performs operations to free up resources and restore the terminal
// to its original state.
func (p *Program) shutdown(kill bool) {
	if p.renderer != nil {
		if kill {
			p.renderer.kill()
		} else {
			p.renderer.stop()
		}
	}

	_ = p.restoreTerminalState()
	if p.restoreOutput != nil {
		_ = p.restoreOutput()
	}
}

// ReleaseTerminal restores the original terminal state and cancels the input
// reader. You can return control to the Program with RestoreTerminal.
func (p *Program) ReleaseTerminal() error {
	p.ignoreSignals = true
	p.cancelReader.Cancel()
	p.waitForReadLoop()

	p.altScreenWasActive = p.renderer.altScreen()
	return p.restoreTerminalState()
}

// RestoreTerminal reinitializes the Program's input reader, restores the
// terminal to the former state when the program was running, and repaints.
// Use it to reinitialize a Program after running ReleaseTerminal.
func (p *Program) RestoreTerminal() error {
	p.ignoreSignals = false

	if err := p.initTerminal(); err != nil {
		return err
	}
	if err := p.initCancelReader(); err != nil {
		return err
	}

	if p.altScreenWasActive {
		p.renderer.enterAltScreen()
	} else {
		// entering alt screen already causes a repaint.
		go p.Send(repaintMsg{})
	}

	return nil
}

// Println prints above the Program. This output is unmanaged by the program
// and will persist across renders by the Program.
//
// If the altscreen is active no output will be printed.
func (p *Program) Println(args ...interface{}) {
	p.msgs <- printLineMessage{
		messageBody: fmt.Sprint(args...),
	}
}

// Printf prints above the Program. It takes a format template followed by
// values similar to fmt.Printf. This output is unmanaged by the program and
// will persist across renders by the Program.
//
// Unlike fmt.Printf (but similar to log.Printf) the message will be print on
// its own line.
//
// If the altscreen is active no output will be printed.
func (p *Program) Printf(template string, args ...interface{}) {
	p.msgs <- printLineMessage{
		messageBody: fmt.Sprintf(template, args...),
	}
}

// Adds a handler to the list of handlers. We wait for all handlers to terminate
// gracefully on shutdown.
func (h *handlers) add(ch chan struct{}) {
	*h = append(*h, ch)
}

// Shutdown waits for all handlers to terminate.
func (h handlers) shutdown() {
	var wg sync.WaitGroup
	for _, ch := range h {
		wg.Add(1)
		go func(ch chan struct{}) {
			<-ch
			wg.Done()
		}(ch)
	}
	wg.Wait()
}
