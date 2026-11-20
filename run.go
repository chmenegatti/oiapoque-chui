package main

import (
	"fmt"
	"time"

	"github.com/gdamore/tcell/v2"
)

// runLocal joga no terminal atual.
func RunLocal(routes []*Route) error {
	tty, err := tcell.NewDevTty()
	if err != nil {
		return fmt.Errorf("erro ao abrir terminal: %w", err)
	}
	s, kb, err := openScreen(tty, nil)
	if err != nil {
		return fmt.Errorf("erro ao abrir terminal: %w", err)
	}
	app := NewApp(routes, SavePath())
	app.kb = kb
	run(s, app, nil)
	app.Close()
	closeScreen(s, tty)
	return nil
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
