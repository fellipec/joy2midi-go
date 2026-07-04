package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Tipo de alvo MIDI para uma entrada do joystick.
type targetKind int

const (
	targetIgnore targetKind = iota
	targetNote              // botão -> note on/off
	targetControl           // eixo ou botão -> control change
	targetBend              // eixo -> pitch bend
	targetMMC               // botão/eixo -> comando MMC (transporte)
	targetPulse             // eixo bidirecional -> 2 CCs diferentes, um por direção
	                         // (pra vincular ações do editor via binding map do Ardour,
	                         // tipo zoom in/out ou scroll left/right)
	targetSplit              // eixo bidirecional -> 2 CCs CONTÍNUOS, um por metade
	                          // (ex: empurrar = expression, puxar = mod wheel,
	                          //  ao invés de desperdiçar metade do curso do eixo)
)

// mmcCommands mapeia nomes legíveis pros bytes de comando MMC padrão
// (ver MMA MIDI Machine Control spec / Ardour: Preferences > Transport > Chase).
var mmcCommands = map[string]byte{
	"stop":          0x01,
	"play":          0x02,
	"deferred_play": 0x03,
	"ffwd":          0x04,
	"rewind":        0x05,
	"record":        0x06, // record strobe (punch in)
	"record_exit":   0x07, // punch out
	"record_pause":  0x08,
	"pause":         0x09,
	"eject":         0x0A,
}

// axisDir controla como um eixo bidirecional (-32767..32767) é escalado
// pra um CC (0..127) quando o alvo é "control".
type axisDir int

const (
	dirBipolar axisDir = iota // padrão: centro=64, extremos=0/127 (bom pra pan, por ex.)
	dirUp                     // centro=0, só a metade positiva sobe até 127 (mod wheel)
	dirDown                   // centro=0, só a metade negativa sobe até 127
	dirAbs                    // centro=0, QUALQUER direção sobe até 127 (usa o curso todo)
)

type mapping struct {
	kind targetKind
	arg  int     // número da nota/CC, ou comando MMC positivo, conforme kind
	arg2 int     // só usado em targetMMC de eixo: comando pro lado negativo (-1 = nenhum)
	dir  axisDir // só relevante quando kind == targetControl e a origem é um eixo
}

type Config struct {
	Channel  int // 0-15 (canal MIDI 1-16)
	Deadzone int // zona morta em torno do centro do eixo (-32767..32767)

	axisMap   map[int]mapping
	buttonMap map[int]mapping
}

func defaultConfig() *Config {
	return &Config{
		Channel:   0,
		Deadzone:  1500,
		axisMap:   map[int]mapping{},
		buttonMap: map[int]mapping{},
	}
}

// loadConfig lê um arquivo de mapeamento no formato:
//
//	# comentário
//	channel = 1
//	deadzone = 2000
//	axis 0 => bend
//	axis 1 => control 1
//	axis 2 => ignore
//	button 0 => note 60
//	button 1 => note 62
//	button 2 => control 64      # ex.: pedal de sustain como CC64 liga/desliga
func loadConfig(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("abrindo config %s: %w", path, err)
	}
	defer f.Close()

	cfg := defaultConfig()
	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if i := strings.Index(line, "#"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if line == "" {
			continue
		}

		if strings.Contains(line, "=>") {
			if err := parseMappingLine(cfg, line); err != nil {
				return nil, fmt.Errorf("linha %d: %w", lineNo, err)
			}
			continue
		}

		if strings.Contains(line, "=") {
			if err := parseSettingLine(cfg, line); err != nil {
				return nil, fmt.Errorf("linha %d: %w", lineNo, err)
			}
			continue
		}

		return nil, fmt.Errorf("linha %d: não entendi %q", lineNo, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func parseSettingLine(cfg *Config, line string) error {
	parts := strings.SplitN(line, "=", 2)
	key := strings.TrimSpace(parts[0])
	val := strings.TrimSpace(parts[1])
	n, err := strconv.Atoi(val)
	if err != nil {
		return fmt.Errorf("valor inválido para %q: %v", key, err)
	}
	switch key {
	case "channel":
		if n < 1 || n > 16 {
			return fmt.Errorf("channel deve ser 1-16, recebi %d", n)
		}
		cfg.Channel = n - 1
	case "deadzone":
		cfg.Deadzone = n
	default:
		return fmt.Errorf("configuração desconhecida %q", key)
	}
	return nil
}

func parseMappingLine(cfg *Config, line string) error {
	sides := strings.SplitN(line, "=>", 2)
	left := strings.Fields(strings.TrimSpace(sides[0]))
	right := strings.Fields(strings.TrimSpace(sides[1]))
	if len(left) != 2 || len(right) == 0 {
		return fmt.Errorf("formato inválido %q", line)
	}

	kind := left[0]
	idx, err := strconv.Atoi(left[1])
	if err != nil {
		return fmt.Errorf("índice inválido em %q: %v", line, err)
	}

	m, err := parseTarget(right)
	if err != nil {
		return err
	}

	switch kind {
	case "axis":
		cfg.axisMap[idx] = m
	case "button":
		cfg.buttonMap[idx] = m
	default:
		return fmt.Errorf("tipo de entrada desconhecido %q (use axis/button)", kind)
	}
	return nil
}

func parseTarget(tokens []string) (mapping, error) {
	switch tokens[0] {
	case "ignore":
		return mapping{kind: targetIgnore}, nil
	case "note":
		if len(tokens) < 2 {
			return mapping{}, fmt.Errorf("'note' precisa de um número (0-127)")
		}
		n, err := strconv.Atoi(tokens[1])
		if err != nil || n < 0 || n > 127 {
			return mapping{}, fmt.Errorf("nota inválida %q", tokens[1])
		}
		return mapping{kind: targetNote, arg: n}, nil
	case "control":
		if len(tokens) < 2 {
			return mapping{}, fmt.Errorf("'control' precisa de um número de CC (0-127)")
		}
		n, err := strconv.Atoi(tokens[1])
		if err != nil || n < 0 || n > 127 {
			return mapping{}, fmt.Errorf("CC inválido %q", tokens[1])
		}
		dir := dirBipolar
		if len(tokens) >= 3 {
			switch tokens[2] {
			case "up":
				dir = dirUp
			case "down":
				dir = dirDown
			case "abs":
				dir = dirAbs
			case "bipolar":
				dir = dirBipolar
			default:
				return mapping{}, fmt.Errorf("direção desconhecida %q (use up/down/abs/bipolar)", tokens[2])
			}
		}
		return mapping{kind: targetControl, arg: n, dir: dir}, nil
	case "bend":
		return mapping{kind: targetBend, arg2: -1}, nil
	case "mmc":
		// button => mmc play              (um comando, disparado ao pressionar)
		// axis   => mmc ffwd rewind        (positivo=ffwd, negativo=rewind)
		if len(tokens) < 2 {
			return mapping{}, fmt.Errorf("'mmc' precisa de pelo menos um comando (ex: play, stop, record, rewind, ffwd, pause)")
		}
		cmd1, ok := mmcCommands[tokens[1]]
		if !ok {
			return mapping{}, fmt.Errorf("comando mmc desconhecido %q (opções: stop, play, ffwd, rewind, record, record_exit, record_pause, pause, eject)", tokens[1])
		}
		arg2 := -1
		if len(tokens) >= 3 {
			cmd2, ok := mmcCommands[tokens[2]]
			if !ok {
				return mapping{}, fmt.Errorf("comando mmc desconhecido %q", tokens[2])
			}
			arg2 = int(cmd2)
		}
		return mapping{kind: targetMMC, arg: int(cmd1), arg2: arg2}, nil
	case "pulse":
		// axis => pulse CC_POSITIVO CC_NEGATIVO
		// Manda CC=127 uma vez ao cruzar pra cada lado, CC=0 ao voltar pro
		// centro. Pensado pra vincular ações do editor do Ardour (zoom,
		// scroll) via binding map -- não é pra controle contínuo.
		if len(tokens) != 3 {
			return mapping{}, fmt.Errorf("'pulse' precisa de exatamente 2 números de CC (positivo e negativo)")
		}
		ccPos, err := strconv.Atoi(tokens[1])
		if err != nil || ccPos < 0 || ccPos > 127 {
			return mapping{}, fmt.Errorf("CC positivo inválido %q", tokens[1])
		}
		ccNeg, err := strconv.Atoi(tokens[2])
		if err != nil || ccNeg < 0 || ccNeg > 127 {
			return mapping{}, fmt.Errorf("CC negativo inválido %q", tokens[2])
		}
		return mapping{kind: targetPulse, arg: ccPos, arg2: ccNeg}, nil
	case "split":
		// axis => split CC_POSITIVO CC_NEGATIVO
		// Cada metade do curso do eixo manda um CC contínuo diferente
		// (0-127 conforme a distância do centro), em vez de desperdiçar
		// metade do curso como o modo up/down de 'control' faz.
		if len(tokens) != 3 {
			return mapping{}, fmt.Errorf("'split' precisa de exatamente 2 números de CC (positivo e negativo)")
		}
		ccPos, err := strconv.Atoi(tokens[1])
		if err != nil || ccPos < 0 || ccPos > 127 {
			return mapping{}, fmt.Errorf("CC positivo inválido %q", tokens[1])
		}
		ccNeg, err := strconv.Atoi(tokens[2])
		if err != nil || ccNeg < 0 || ccNeg > 127 {
			return mapping{}, fmt.Errorf("CC negativo inválido %q", tokens[2])
		}
		return mapping{kind: targetSplit, arg: ccPos, arg2: ccNeg}, nil
	default:
		return mapping{}, fmt.Errorf("alvo desconhecido %q (use note/control/bend/mmc/pulse/split/ignore)", tokens[0])
	}
}
