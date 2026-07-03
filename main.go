// joy2midi-go: usa um joystick/gamepad Linux como controlador MIDI via ALSA
// sequencer. Pensado pra rodar junto com PipeWire-JACK / Ardour.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	device := flag.String("device", "/dev/input/js0", "dispositivo do joystick")
	configPath := flag.String("config", "", "arquivo de mapeamento (obrigatório)")
	clientName := flag.String("name", "joy2midi-go", "nome do cliente ALSA sequencer")
	verbose := flag.Bool("v", false, "log de cada evento recebido/enviado")
	flag.Parse()

	if *configPath == "" {
		fmt.Fprintln(os.Stderr, "uso: joy2midi-go -config mapa.cfg [-device /dev/input/js0] [-name meu-nome]")
		os.Exit(2)
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	js, err := openJoystick(*device)
	if err != nil {
		log.Fatalf("joystick: %v", err)
	}
	defer js.Close()

	midi, err := openMidiOut(*clientName)
	if err != nil {
		log.Fatalf("midi: %v", err)
	}
	defer midi.Close()

	log.Printf("rodando: %s -> porta ALSA %q, canal MIDI %d", *device, *clientName, cfg.Channel+1)
	log.Printf("conecte a porta com: aconnect '%s' 'nome-do-destino'  (ou use QjackCtl/Carla)", *clientName)

	// Encerra graciosamente com Ctrl+C, garantindo NoteOff pendentes não ficam presos.
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigc
		log.Println("encerrando...")
		midi.Close()
		os.Exit(0)
	}()

	run(js, midi, cfg, *verbose)
}

func run(js *Joystick, midi *MidiOut, cfg *Config, verbose bool) {
	for {
		ev, err := js.ReadEvent()
		if err != nil {
			if errors.Is(err, io.EOF) {
				log.Println("joystick desconectado")
				return
			}
			log.Fatalf("lendo joystick: %v", err)
		}

		switch {
		case ev.IsButton():
			handleButton(midi, cfg, int(ev.Number), ev.Value, verbose)
		case ev.IsAxis():
			handleAxis(midi, cfg, int(ev.Number), ev.Value, verbose)
		default:
			// evento desconhecido, ignora
		}
	}
}

func handleButton(midi *MidiOut, cfg *Config, number int, value int16, verbose bool) {
	m, ok := cfg.buttonMap[number]
	if !ok || m.kind == targetIgnore {
		return
	}
	pressed := value != 0

	switch m.kind {
	case targetNote:
		if pressed {
			midi.NoteOn(cfg.Channel, m.arg, 127)
			if verbose {
				log.Printf("button %d -> note %d ON", number, m.arg)
			}
		} else {
			midi.NoteOff(cfg.Channel, m.arg)
			if verbose {
				log.Printf("button %d -> note %d OFF", number, m.arg)
			}
		}
	case targetControl:
		// botão como CC liga/desliga (0 ou 127) — útil p/ sustain, por exemplo.
		v := 0
		if pressed {
			v = 127
		}
		midi.ControlChange(cfg.Channel, m.arg, v)
		if verbose {
			log.Printf("button %d -> CC%d = %d", number, m.arg, v)
		}
	case targetMMC:
		// comandos MMC são disparos únicos (não têm "estado"), então só
		// mandamos ao pressionar, ignorando o soltar do botão.
		if pressed {
			midi.SendMMC(byte(m.arg))
			if verbose {
				log.Printf("button %d -> MMC 0x%02X", number, m.arg)
			}
		}
	}
}

// axisZone guarda o último "lado" (-1/0/1) que cada eixo mapeado pra MMC
// estava, pra só disparar o comando na transição (borda), não a cada evento
// enquanto o eixo continua deslocado do centro.
var axisZone = map[int]int{}

// axisRawMax é o valor absoluto máximo reportado pela API de joystick do
// kernel para um eixo (int16 signed).
const axisRawMax = 32767

func handleAxis(midi *MidiOut, cfg *Config, number int, value int16, verbose bool) {
	m, ok := cfg.axisMap[number]
	if !ok || m.kind == targetIgnore {
		return
	}

	// zona morta em torno do centro
	if abs16(value) < int16(cfg.Deadzone) {
		value = 0
	}

	switch m.kind {
	case targetControl:
		v := scaleToCC(value, m.dir)
		midi.ControlChange(cfg.Channel, m.arg, v)
		if verbose {
			log.Printf("axis %d -> CC%d = %d", number, m.arg, v)
		}
	case targetBend:
		v := scaleToBend(value)
		midi.PitchBend(cfg.Channel, v)
		if verbose {
			log.Printf("axis %d -> pitchbend %d", number, v)
		}
	case targetNote:
		// pouco comum, mas permite usar um eixo como gatilho binário de nota
		// (cruzando o centro liga/desliga a nota).
		if value > int16(cfg.Deadzone) {
			midi.NoteOn(cfg.Channel, m.arg, 127)
		} else {
			midi.NoteOff(cfg.Channel, m.arg)
		}
	case targetMMC:
		zone := 0
		if value > int16(cfg.Deadzone) {
			zone = 1
		} else if value < -int16(cfg.Deadzone) {
			zone = -1
		}
		prev := axisZone[number]
		if zone != prev {
			axisZone[number] = zone
			switch {
			case zone == 1:
				midi.SendMMC(byte(m.arg))
				if verbose {
					log.Printf("axis %d -> MMC 0x%02X (positivo)", number, m.arg)
				}
			case zone == -1 && m.arg2 >= 0:
				midi.SendMMC(byte(m.arg2))
				if verbose {
					log.Printf("axis %d -> MMC 0x%02X (negativo)", number, m.arg2)
				}
			}
			// zone == 0 (voltou pro centro): não dispara nada
		}
	}
}

// scaleToCC mapeia o valor bruto do eixo (-32767..32767) para 0..127,
// de acordo com o modo de direção configurado:
//
//   - dirBipolar: centro=64, extremo negativo=0, extremo positivo=127.
//     Bom pra pan, balance, etc. -- coisas onde o centro É um valor útil.
//   - dirUp:   centro=0, só o lado positivo sobe até 127; lado negativo trava em 0.
//   - dirDown: centro=0, só o lado negativo sobe até 127; lado positivo trava em 0.
//   - dirAbs:  centro=0, QUALQUER lado sobe até 127 (usa o curso inteiro do eixo,
//     mas os dois extremos produzem o mesmo valor de saída).
//
// dirUp/dirDown são o modo certo pra emular um mod wheel de verdade: em
// repouso o eixo manda CC=0 (sem vibrato nenhum), só sobe quando você empurra.
func scaleToCC(v int16, dir axisDir) int {
	switch dir {
	case dirUp:
		if v <= 0 {
			return 0
		}
		return clamp(int(v)*127/axisRawMax, 0, 127)
	case dirDown:
		if v >= 0 {
			return 0
		}
		return clamp(int(-v)*127/axisRawMax, 0, 127)
	case dirAbs:
		av := v
		if av < 0 {
			av = -av
		}
		return clamp(int(av)*127/axisRawMax, 0, 127)
	default: // dirBipolar
		scaled := (int(v) + axisRawMax) * 127 / (2 * axisRawMax)
		return clamp(scaled, 0, 127)
	}
}

// scaleToBend mapeia -32767..32767 para -8192..8191 (faixa de pitch bend MIDI de 14 bits,
// centrada em 0 para a API do ALSA sequencer).
func scaleToBend(v int16) int {
	scaled := int(v) * 8192 / axisRawMax
	return clamp(scaled, -8192, 8191)
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func abs16(v int16) int16 {
	if v < 0 {
		return -v
	}
	return v
}
