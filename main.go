package main

import (
	"embed"
	"flag"
	"fmt"
	"os"
)

//go:embed rotas/*.json
var rotasFS embed.FS

func main() {
	addr := flag.String("ssh", "", "serve o jogo por ssh neste endereço (ex: :2323)")
	mode3d := flag.Bool("3d", false, "abre a versão 3D em janela (protótipo)")
	trecho := flag.Int("trecho", 8, "trecho da versão 3D (1 a 10)")
	flag.Parse()

	routes, err := LoadRoutes(rotasFS)
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro ao carregar rotas:", err)
		os.Exit(1)
	}
	if *mode3d {
		if err := Run3D(routes, *trecho); err != nil {
			fmt.Fprintln(os.Stderr, "erro na versão 3D:", err)
			os.Exit(1)
		}
		return
	}
	if *addr != "" {
		if err := ServeSSH(*addr, routes); err != nil {
			fmt.Fprintln(os.Stderr, "erro no servidor ssh:", err)
			os.Exit(1)
		}
		return
	}

	if err := RunLocal(routes); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
