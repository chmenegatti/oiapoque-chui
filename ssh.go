package main

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"charm.land/ssh"
	"charm.land/wish/v2"
	"charm.land/wish/v2/activeterm"
	"charm.land/wish/v2/logging"
	"github.com/gdamore/tcell/v2"
	gossh "golang.org/x/crypto/ssh"
)

// serveSSH serve o jogo por ssh: `ssh -p 2323 host`. Cada sessão tem seu
// próprio jogo; quem entra com chave pública tem progresso salvo em saves/.
func serveSSH(addr string, routes []*Route) error {
	os.Setenv("COLORTERM", "truecolor") // terminais modernos aguentam; tcell lê daqui
	srv, err := wish.NewServer(
		wish.WithAddress(addr),
		wish.WithHostKeyPath(".ssh/oiapoque_chui_ed25519"),
		wish.WithPublicKeyAuth(func(ssh.Context, ssh.PublicKey) bool { return true }),
		wish.WithKeyboardInteractiveAuth(func(ssh.Context, gossh.KeyboardInteractiveChallenge) bool { return true }),
		wish.WithIdleTimeout(30*time.Minute),
		wish.WithMiddleware(
			gameMiddleware(routes),
			activeterm.Middleware(), // exige pty
			logging.Middleware(),
		),
	)
	if err != nil {
		return err
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Printf("servindo em %s · conecte com: ssh -p %s localhost", addr, strings.TrimPrefix(addr[strings.LastIndex(addr, ":"):], ":"))
	select {
	case err := <-errc:
		if !errors.Is(err, ssh.ErrServerClosed) {
			return err
		}
	case <-sig:
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}

func gameMiddleware(routes []*Route) wish.Middleware {
	return func(next ssh.Handler) ssh.Handler {
		return func(sess ssh.Session) {
			pty, winCh, _ := sess.Pty()
			tty := newSessionTty(sess, pty.Window, winCh)
			ti, err := tcell.LookupTerminfo(pty.Term)
			if err != nil {
				ti, _ = tcell.LookupTerminfo("xterm-256color")
			}
			scr, kb, err := openScreen(tty, ti)
			if err != nil {
				wish.Fatalln(sess, "erro ao abrir tela:", err)
				return
			}
			app := NewApp(routes, sshSavePath(sess))
			app.kb = kb
			run(scr, app, sess.Context().Done())
			app.Close()
			km := 0.0
			if app.game != nil {
				km = app.game.km()
			}
			log.Printf("sessão %s: teclado kitty=%v, km %.2f", sess.User(), kb.Active(), km)
			closeScreen(scr, tty)
			next(sess)
		}
	}
}

// sshSavePath: um save por chave pública; sem chave, joga sem salvar.
func sshSavePath(sess ssh.Session) string {
	k := sess.PublicKey()
	if k == nil {
		return ""
	}
	fp := strings.NewReplacer("/", "_", "+", "-", ":", "_").Replace(gossh.FingerprintSHA256(k))
	return filepath.Join("saves", fp+".json")
}

// sessionTty adapta uma sessão ssh à interface tcell.Tty.
type sessionTty struct {
	sess ssh.Session

	mu  sync.Mutex
	win ssh.Window
	cb  func()

	in     chan []byte   // dados lidos da sessão
	drain  chan struct{} // fechado por Drain: destrava Read
	closed chan struct{}
	once   sync.Once
	buf    []byte
}

func newSessionTty(sess ssh.Session, w ssh.Window, winCh <-chan ssh.Window) *sessionTty {
	t := &sessionTty{sess: sess, win: w, in: make(chan []byte), closed: make(chan struct{})}
	go func() { // tamanho da janela
		for w := range winCh {
			t.mu.Lock()
			t.win = w
			cb := t.cb
			t.mu.Unlock()
			if cb != nil {
				cb()
			}
		}
	}()
	go func() { // leitura da sessão num goroutine próprio, para Read poder ser interrompido
		defer close(t.closed)
		for {
			b := make([]byte, 256)
			n, err := sess.Read(b)
			if n > 0 {
				select {
				case t.in <- b[:n]:
				case <-sess.Context().Done():
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	return t
}

func (t *sessionTty) Start() error {
	t.mu.Lock()
	t.drain = make(chan struct{})
	t.mu.Unlock()
	return nil
}

func (t *sessionTty) Stop() error { return nil }

func (t *sessionTty) Drain() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	select {
	case <-t.drain:
	default:
		close(t.drain)
	}
	return nil
}

func (t *sessionTty) NotifyResize(cb func()) {
	t.mu.Lock()
	t.cb = cb
	t.mu.Unlock()
}

func (t *sessionTty) WindowSize() (tcell.WindowSize, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return tcell.WindowSize{Width: t.win.Width, Height: t.win.Height}, nil
}

func (t *sessionTty) Read(p []byte) (int, error) {
	if len(t.buf) == 0 {
		t.mu.Lock()
		drain := t.drain
		t.mu.Unlock()
		select {
		case b := <-t.in:
			t.buf = b
		case <-drain:
			return 0, io.EOF
		case <-t.closed:
			return 0, io.EOF
		}
	}
	n := copy(p, t.buf)
	t.buf = t.buf[n:]
	return n, nil
}

func (t *sessionTty) Write(p []byte) (int, error) { return t.sess.Write(p) }

func (t *sessionTty) Close() error { return nil }
