# Bubble Tea

<p>
    <img src="https://stuff.charm.sh/bubbletea/bubbletea-github-header-simple.png" width="313" alt="Bubble Tea Title Treatment"><br>
    <a href="https://github.com/charmbracelet/bubbletea/releases"><img src="https://img.shields.io/github/release/charmbracelet/bubbletea.svg" alt="Latest Release"></a>
    <a href="https://pkg.go.dev/github.com/charmbracelet/bubbletea?tab=doc"><img src="https://godoc.org/github.com/golang/gddo?status.svg" alt="GoDoc"></a>
    <a href="https://github.com/charmbracelet/bubbletea/actions"><img src="https://github.com/charmbracelet/bubbletea/workflows/build/badge.svg" alt="Build Status"></a>
</p>

The fun, functional and stateful way to build terminal apps. A Go framework
based on [The Elm Architecture][elm]. Bubble Tea is well-suited for simple and
complex terminal applications, either inline, full-window, or a mix of both.

<picture>
  <source media="(max-width: 800px)" srcset="https://stuff.charm.sh/bubbletea/bubbletea-example.gif?0">
  <source media="(min-width: 800px)" width="800" srcset="https://stuff.charm.sh/bubbletea/bubbletea-example.gif?0">
  <img src="https://stuff.charm.sh/bubbletea/bubbletea-example.gif?0" alt="Bubble Tea Example">
</picture>

Bubble Tea is in use in production and includes a number of features and
performance optimizations we’ve added along the way. Among those is a standard
framerate-based renderer, a renderer for high-performance scrollable
regions which works alongside the main renderer, and mouse support.

To get started, see the tutorial below, the [examples][examples], the
[docs][docs], the [video tutorials][youtube] and some common [resources](#libraries-we-use-with-bubble-tea).

[youtube]: https://charm.sh/yt

## By the way

Be sure to check out [Bubbles][bubbles], a library of common UI components for Bubble Tea.

<p>
    <a href="https://github.com/charmbracelet/bubbles"><img src="https://stuff.charm.sh/bubbles/bubbles-badge.png" width="174" alt="Bubbles Badge"></a>&nbsp;&nbsp;
    <a href="https://github.com/charmbracelet/bubbles"><img src="https://stuff.charm.sh/bubbles-examples/textinput.gif" width="400" alt="Text Input Example from Bubbles"></a>
</p>

* * *

## Tutorial

Bubble Tea is based on the functional design paradigms of [The Elm
Architecture][elm], which happens to work nicely with Go. It's a delightful way
to build applications.

This tutorial assumes you have a working knowledge of Go.

By the way, the non-annotated source code for this program is available
[on GitHub][tut-source].

[elm]: https://guide.elm-lang.org/architecture/
[tut-source]:https://github.com/charmbracelet/bubbletea/tree/master/tutorials/basics

### Enough! Let's get to it.

For this tutorial, we're making a shopping list.

To start we'll define our package and import some libraries. Our only external
import will be the Bubble Tea library, which we'll call `tea` for short.

```go
package main

import (
    "fmt"
    "os"

    tea "github.com/charmbracelet/bubbletea"
)
```

Bubble Tea programs are comprised of a **model** that describes the application
state and three simple methods on that model:

* **Init**, a function that returns an initial command for the application to run.
* **Update**, a function that handles incoming events and updates the model accordingly.
* **View**, a function that renders the UI based on the data in the model.

### The Model

So let's start by defining our model which will store our application's state.
It can be any type, but a `struct` usually makes the most sense.

```go
type model struct {
    choices  []string           // items on the to-do list
    cursor   int                // which to-do list item our cursor is pointing at
    selected map[int]struct{}   // which to-do items are selected
}
```

### Initialization

Next, we’ll define our application’s initial state. In this case, we’re defining
a function to return our initial model, however, we could just as easily define
the initial model as a variable elsewhere, too.

```go
func initialModel() model {
	return model{
		// Our to-do list is a grocery list
		choices:  []string{"Buy carrots", "Buy celery", "Buy kohlrabi"},

		// A map which indicates which choices are selected. We're using
		// the  map like a mathematical set. The keys refer to the indexes
		// of the `choices` slice, above.
		selected: make(map[int]struct{}),
	}
}
```

Next, we define the `Init` method. `Init` can return a `Cmd` that could perform
some initial I/O. For now, we don't need to do any I/O, so for the command,
we'll just return `nil`, which translates to "no command."

```go
func (m model) Init() tea.Cmd {
    // Just return `nil`, which means "no I/O right now, please."
    return nil
}
```

### The Update Method

Next up is the update method. The update function is called when ”things
happen.” Its job is to look at what has happened and return an updated model in
response. It can also return a `Cmd` to make more things happen, but for now
don't worry about that part.

In our case, when a user presses the down arrow, `Update`’s job is to notice
that the down arrow was pressed and move the cursor accordingly (or not).

The “something happened” comes in the form of a `Msg`, which can be any type.
Messages are the result of some I/O that took place, such as a keypress, timer
tick, or a response from a server.

We usually figure out which type of `Msg` we received with a type switch, but
you could also use a type assertion.

For now, we'll just deal with `tea.KeyMsg` messages, which are automatically
sent to the update function when keys are pressed.

```go
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {

    // Is it a key press?
    case tea.KeyMsg:

        // Cool, what was the actual key pressed?
        switch msg.String() {

        // These keys should exit the program.
        case "ctrl+c", "q":
            return m, tea.Quit

        // The "up" and "k" keys move the cursor up
        case "up", "k":
            if m.cursor > 0 {
                m.cursor--
            }

        // The "down" and "j" keys move the cursor down
        case "down", "j":
            if m.cursor < len(m.choices)-1 {
                m.cursor++
            }

        // The "enter" key and the spacebar (a literal space) toggle
        // the selected state for the item that the cursor is pointing at.
        case "enter", " ":
            _, ok := m.selected[m.cursor]
            if ok {
                delete(m.selected, m.cursor)
            } else {
                m.selected[m.cursor] = struct{}{}
            }
        }
    }

    // Return the updated model to the Bubble Tea runtime for processing.
    // Note that we're not returning a command.
    return m, nil
}
```

You may have noticed that <kbd>ctrl+c</kbd> and <kbd>q</kbd> above return
a `tea.Quit` command with the model. That’s a special command which instructs
the Bubble Tea runtime to quit, exiting the program.

### The View Method

At last, it’s time to render our UI. Of all the methods, the view is the
simplest. We look at the model in its current state and use it to return
a `string`. That string is our UI!

Because the view describes the entire UI of your application, you don’t have to
worry about redrawing logic and stuff like that. Bubble Tea takes care of it
for you.

```go
func (m model) View() string {
    // The header
    s := "What should we buy at the market?\n\n"

    // Iterate over our choices
    for i, choice := range m.choices {

        // Is the cursor pointing at this choice?
        cursor := " " // no cursor
        if m.cursor == i {
            cursor = ">" // cursor!
        }

        // Is this choice selected?
        checked := " " // not selected
        if _, ok := m.selected[i]; ok {
            checked = "x" // selected!
        }

        // Render the row
        s += fmt.Sprintf("%s [%s] %s\n", cursor, checked, choice)
    }

    // The footer
    s += "\nPress q to quit.\n"

    // Send the UI for rendering
    return s
}
```

### All Together Now

The last step is to simply run our program. We pass our initial model to
`tea.NewProgram` and let it rip:

```go
func main() {
    p := tea.NewProgram(initialModel())
    if _, err := p.Run(); err != nil {
        fmt.Printf("Alas, there's been an error: %v", err)
        os.Exit(1)
    }
}
```

## What’s Next?

This tutorial covers the basics of building an interactive terminal UI, but
in the real world you'll also need to perform I/O. To learn about that have a
look at the [Command Tutorial][cmd]. It's pretty simple.

There are also several [Bubble Tea examples][examples] available and, of course,
there are [Go Docs][docs].

[cmd]: http://github.com/charmbracelet/bubbletea/tree/master/tutorials/commands/
[examples]: http://github.com/charmbracelet/bubbletea/tree/master/examples
[docs]: https://pkg.go.dev/github.com/charmbracelet/bubbletea?tab=doc

## Debugging

### Debugging with Delve

Since Bubble Tea apps assume control of stdin and stdout, you’ll need to run
delve in headless mode and then connect to it:

```bash
# Start the debugger
$ dlv debug --headless .
API server listening at: 127.0.0.1:34241

# Connect to it from another terminal
$ dlv connect 127.0.0.1:34241
```

Note that the default port used will vary on your system and per run, so
actually watch out what address the first `dlv` run tells you to connect to.

### Logging Stuff

You can’t really log to stdout with Bubble Tea because your TUI is busy
occupying that! You can, however, log to a file by including something like
the following prior to starting your Bubble Tea program:

```go
if len(os.Getenv("DEBUG")) > 0 {
	f, err := tea.LogToFile("debug.log", "debug")
	if err != nil {
		fmt.Println("fatal:", err)
		os.Exit(1)
	}
	defer f.Close()
}
```

To see what’s being logged in real time, run `tail -f debug.log` while you run
your program in another window.

## Libraries we use with Bubble Tea

* [Bubbles][bubbles]: Common Bubble Tea components such as text inputs, viewports, spinners and so on
* [Lip Gloss][lipgloss]: Style, format and layout tools for terminal applications
* [Harmonica][harmonica]: A spring animation library for smooth, natural motion
* [BubbleZone][bubblezone]: Easy mouse event tracking for Bubble Tea components
* [Termenv][termenv]: Advanced ANSI styling for terminal applications
* [Reflow][reflow]: Advanced ANSI-aware methods for working with text

[bubbles]: https://github.com/charmbracelet/bubbles
[lipgloss]: https://github.com/charmbracelet/lipgloss
[harmonica]: https://github.com/charmbracelet/harmonica
[bubblezone]: https://github.com/lrstanley/bubblezone
[termenv]: https://github.com/muesli/termenv
[reflow]: https://github.com/muesli/reflow

## Bubble Tea in the Wild

For some Bubble Tea programs in production, see:

* [AT CLI](https://github.com/daskycodes/at_cli): a utility for executing AT Commands via serial port connections
* [Aztify](https://github.com/Azure/aztfy): bring Microsoft Azure resources under Terraform
* [Canard](https://github.com/mrusme/canard): an RSS client
* [charm](https://github.com/charmbracelet/charm): the official Charm user account manager
* [chezmoi](https://github.com/twpayne/chezmoi): manage your dotfiles across multiple machines, securely
* [circumflex](https://github.com/bensadeh/circumflex): read Hacker News in your terminal
* [clidle](https://github.com/ajeetdsouza/clidle): a Wordle clone for your terminal
* [container-canary](https://github.com/NVIDIA/container-canary): a container validator
* [dns53](https://github.com/purpleclay/dns53): dynamic DNS with Amazon Route53. Expose your EC2 quickly, securely and privately
* [flapioca](https://github.com/kbrgl/flapioca): Flappy Bird on the CLI!
* [fm](https://github.com/knipferrc/fm): a terminal-based file manager
* [fork-cleaner](https://github.com/caarlos0/fork-cleaner): cleans up old and inactive forks in your GitHub account
* [fztea](https://github.com/jon4hz/fztea): connect to your Flipper's UI over serial or make it accessible via SSH
* [gambit](https://github.com/maaslalani/gambit): play chess in the terminal
* [gembro](https://git.sr.ht/~rafael/gembro): a mouse-driven Gemini browser
* [gh-b](https://github.com/joaom00/gh-b): GitHub CLI extension to easily manage your branches
* [gh-dash](https://www.github.com/dlvhdr/gh-dash): GitHub CLI extension to display a dashboard of PRs and issues
* [gitflow-toolkit](https://github.com/mritd/gitflow-toolkit): a GitFlow submission tool
* [Glow](https://github.com/charmbracelet/glow): a markdown reader, browser and online markdown stash
* [gocovsh](https://github.com/orlangure/gocovsh): explore Go coverage reports from the CLI
* [got](https://github.com/fedeztk/got): a simple translator and text-to-speech app build on top of simplytranslate's APIs
* [httpit](https://github.com/gonetx/httpit): a rapid http(s) benchmark tool
* [IDNT](https://github.com/r-darwish/idnt): batch software uninstaller
* [kboard](https://github.com/CamiloGarciaLaRotta/kboard): a typing game
* [mandelbrot-cli](https://github.com/MicheleFiladelfia/mandelbrot-cli): Multiplatform terminal mandelbrot set explorer
* [mc](https://github.com/minio/mc): the official [MinIO](https://min.io) client
* [mergestat](https://github.com/mergestat/mergestat): run SQL queries on git repositories
* [Noted](https://github.com/torbratsberg/noted): Note viewer and manager
* [pathos](https://github.com/chip/pathos): pathos - CLI for editing a PATH env variable
* [portal](https://github.com/ZinoKader/portal): securely send transfer between computers
* [redis-viewer](https://github.com/SaltFishPr/redis-viewer): browse Redis databases
* [sku](https://github.com/fedeztk/sku): a simple TUI for playing sudoku inside the terminal
* [Slides](https://github.com/maaslalani/slides): a markdown-based presentation tool
* [Soft Serve](https://github.com/charmbracelet/soft-serve): a command-line-first Git server that runs a TUI over SSH
* [StormForge Optimize Controller](https://github.com/thestormforge/optimize-controller): a tool for experimenting with application configurations in Kubernetes
* [STTG](https://github.com/wille1101/sttg): teletext client for SVT, Sweden’s national public television station
* [sttr](https://github.com/abhimanyu003/sttr): run various text transformations
* [tasktimer](https://github.com/caarlos0/tasktimer): a dead-simple task timer
* [termdbms](https://github.com/mathaou/termdbms): a keyboard and mouse driven database browser
* [ticker](https://github.com/achannarasappa/ticker): a terminal stock watcher and stock position tracker
* [tran](https://github.com/abdfnx/tran): securely transfer stuff between computers (based on [portal][portal])
* [Typer](https://github.com/maaslalani/typer): a typing test
* [tz](https://github.com/oz/tz): an aid for scheduling across multiple time zones
* [ugm](https://github.com/ariasmn/ugm): a unix user and group browser
* [wander](https://github.com/robinovitch61/wander): HashiCorp Nomad terminal client
* [wishlist](https://github.com/charmbracelet/wishlist): an SSH directory

## Feedback

We'd love to hear your thoughts on this tutorial. Feel free to drop us a note!

* [Twitter](https://twitter.com/charmcli)
* [The Fediverse](https://mastodon.technology/@charm)

## Acknowledgments

Bubble Tea is based on the paradigms of [The Elm Architecture][elm] by Evan
Czaplicki et alia and the excellent [go-tea][gotea] by TJ Holowaychuk. It’s
inspired by the many great [_Zeichenorientierte Benutzerschnittstellen_][zb]
of days past.

[elm]: https://guide.elm-lang.org/architecture/
[gotea]: https://github.com/tj/go-tea
[zb]: https://de.wikipedia.org/wiki/Zeichenorientierte_Benutzerschnittstelle

## License

[MIT](https://github.com/charmbracelet/bubbletea/raw/master/LICENSE)

* * *

Part of [Charm](https://charm.sh).

<a href="https://charm.sh/"><img alt="The Charm logo" src="https://stuff.charm.sh/charm-badge.jpg" width="400"></a>

Charm 热爱开源 • Charm loves open source
