package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/gdamore/tcell/v2"
)

func main() {
	addr := flag.String("ssh", "", "serve o jogo por ssh neste endereço (ex: :2323)")
	flag.Parse()

	routes, err := loadRoutes()
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro ao carregar rotas:", err)
		os.Exit(1)
	}
	if *addr != "" {
		if err := serveSSH(*addr, routes); err != nil {
			fmt.Fprintln(os.Stderr, "erro no servidor ssh:", err)
			os.Exit(1)
		}
		return
	}

	tty, err := tcell.NewDevTty()
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro ao abrir terminal:", err)
		os.Exit(1)
	}
	s, kb, err := openScreen(tty, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro ao abrir terminal:", err)
		os.Exit(1)
	}
	app := NewApp(routes, savePath())
	app.kb = kb
	run(s, app, nil)
	app.Close()
	closeScreen(s, tty)
}

// run é o loop do jogo (60 FPS) numa tela já iniciada. Termina quando o
// jogador sai ou quando done fecha (ex.: sessão ssh caiu).
func run(s tcell.Screen, app *App, done <-chan struct{}) {
	s.HideCursor()
	events := make(chan tcell.Event, 64)
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			ev := s.PollEvent()
			if ev == nil {
				return
			}
			select {
			case events <- ev:
			case <-stop:
				return
			}
		}
	}()

	tick := time.NewTicker(time.Second / 60)
	defer tick.Stop()
	last := time.Now()
	for {
		select {
		case <-done:
			return
		case ev := <-events:
			switch e := ev.(type) {
			case *tcell.EventKey:
				if app.key(e) {
					return
				}
			case *tcell.EventResize:
				s.Sync()
			}
		case now := <-tick.C:
			dt := min(now.Sub(last).Seconds(), 0.1)
			last = now
			app.tick(dt)
			app.draw(s)
			s.Show()
		}
	}
}
