package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// ---------- progresso salvo ----------

type Save struct {
	Trecho    int             `json:"trecho"`
	Dinheiro  float64         `json:"dinheiro"`
	Gasolina  float64         `json:"gasolina"`
	Melhorias map[string]bool `json:"melhorias"`
	Tempo     float64         `json:"tempo"`
	Multas    int             `json:"multas"`
	Parada    *Checkpoint     `json:"parada,omitempty"`
}

func NewSave() Save {
	return Save{Dinheiro: 500, Gasolina: 20, Melhorias: map[string]bool{}}
}

func SavePath() string {
	d, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(d, "oiapoque-chui", "save.json")
}

func LoadSave(path string) Save {
	s := NewSave()
	if path == "" {
		return s
	}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	if s.Melhorias == nil {
		s.Melhorias = map[string]bool{}
	}
	return s
}

func (s Save) Write(p string) error {
	if p == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return os.WriteFile(p, b, 0o644)
}

// ---------- oficina ----------

type upgrade struct {
	ID, Nome, Desc string
	Preco          float64
}

var Upgrades = []upgrade{
	{"motor", "Motor 1.3 envenenado", "+15 km/h de final e arranque, esquenta mais", 900},
	{"radiador", "Radiador novo", "motor esquenta 20% menos", 400},
	{"tanque", "Tanque extra", "+12 L de autonomia", 350},
	{"pneus", "Pneus de chuva", "escorrega bem menos na chuva e na terra", 450},
	{"farol", "Farol de milha", "enxerga o dobro à noite", 250},
}
