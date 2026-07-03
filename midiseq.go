package main

/*
#cgo LDFLAGS: -lasound
#include <alsa/asoundlib.h>
#include <stdlib.h>

static snd_seq_t *g_seq = NULL;
static int g_port = -1;

static int seq_open(const char *client_name) {
    int err = snd_seq_open(&g_seq, "default", SND_SEQ_OPEN_OUTPUT, 0);
    if (err < 0) return err;
    snd_seq_set_client_name(g_seq, client_name);
    g_port = snd_seq_create_simple_port(g_seq, "out",
        SND_SEQ_PORT_CAP_READ | SND_SEQ_PORT_CAP_SUBS_READ,
        SND_SEQ_PORT_TYPE_MIDI_GENERIC | SND_SEQ_PORT_TYPE_APPLICATION);
    if (g_port < 0) return g_port;
    return 0;
}

static void seq_send_note(int channel, int note, int velocity, int on) {
    if (!g_seq) return;
    snd_seq_event_t ev;
    snd_seq_ev_clear(&ev);
    snd_seq_ev_set_source(&ev, g_port);
    snd_seq_ev_set_subs(&ev);
    snd_seq_ev_set_direct(&ev);
    if (on) {
        snd_seq_ev_set_noteon(&ev, channel, note, velocity);
    } else {
        snd_seq_ev_set_noteoff(&ev, channel, note, velocity);
    }
    snd_seq_event_output(g_seq, &ev);
    snd_seq_drain_output(g_seq);
}

static void seq_send_cc(int channel, int cc, int value) {
    if (!g_seq) return;
    snd_seq_event_t ev;
    snd_seq_ev_clear(&ev);
    snd_seq_ev_set_source(&ev, g_port);
    snd_seq_ev_set_subs(&ev);
    snd_seq_ev_set_direct(&ev);
    snd_seq_ev_set_controller(&ev, channel, cc, value);
    snd_seq_event_output(g_seq, &ev);
    snd_seq_drain_output(g_seq);
}

static void seq_send_pitchbend(int channel, int value) {
    if (!g_seq) return;
    snd_seq_event_t ev;
    snd_seq_ev_clear(&ev);
    snd_seq_ev_set_source(&ev, g_port);
    snd_seq_ev_set_subs(&ev);
    snd_seq_ev_set_direct(&ev);
    snd_seq_ev_set_pitchbend(&ev, channel, value);
    snd_seq_event_output(g_seq, &ev);
    snd_seq_drain_output(g_seq);
}

static void seq_send_mmc(unsigned char device_id, unsigned char cmd) {
    if (!g_seq) return;
    // Frame padrão MMC: F0 7F <device-id> 06 <comando> F7
    // device-id 0x7F = "all call" -- funciona com o ID padrão que o Ardour
    // usa em Preferences > Transport > Chase > Inbound MMC device ID (127).
    unsigned char data[6] = {0xF0, 0x7F, device_id, 0x06, cmd, 0xF7};
    snd_seq_event_t ev;
    snd_seq_ev_clear(&ev);
    snd_seq_ev_set_source(&ev, g_port);
    snd_seq_ev_set_subs(&ev);
    snd_seq_ev_set_direct(&ev);
    snd_seq_ev_set_sysex(&ev, sizeof(data), data);
    snd_seq_event_output(g_seq, &ev);
    snd_seq_drain_output(g_seq);
}

static void seq_close(void) {
    if (g_seq) {
        snd_seq_close(g_seq);
        g_seq = NULL;
    }
}
*/
import "C"
import (
	"fmt"
	"unsafe"
)

// MidiOut é um cliente de saída MIDI via ALSA sequencer.
// A porta criada aparece no `aconnect -l`, no QjackCtl/Carla (se você usa
// a2jmidid ou o bridge ALSA-MIDI do PipeWire) e pode ser conectada
// diretamente ao Ardour.
type MidiOut struct{}

func openMidiOut(clientName string) (*MidiOut, error) {
	cName := C.CString(clientName)
	defer C.free(unsafe.Pointer(cName))
	if rc := C.seq_open(cName); rc < 0 {
		return nil, fmt.Errorf("snd_seq_open falhou (código %d) — o daemon ALSA sequencer está rodando?", rc)
	}
	return &MidiOut{}, nil
}

func (m *MidiOut) Close() {
	C.seq_close()
}

func (m *MidiOut) NoteOn(channel, note, velocity int) {
	C.seq_send_note(C.int(channel), C.int(note), C.int(velocity), 1)
}

func (m *MidiOut) NoteOff(channel, note int) {
	C.seq_send_note(C.int(channel), C.int(note), 0, 0)
}

func (m *MidiOut) ControlChange(channel, cc, value int) {
	C.seq_send_cc(C.int(channel), C.int(cc), C.int(value))
}

func (m *MidiOut) PitchBend(channel, value int) {
	C.seq_send_pitchbend(C.int(channel), C.int(value))
}

// SendMMC envia um comando padrão de MIDI Machine Control (transporte).
// O Ardour entende isso nativamente na porta ardour:MMC in, sem precisar
// de MIDI Learn -- só habilitar "Respond to MMC commands" nas preferências.
func (m *MidiOut) SendMMC(cmd byte) {
	C.seq_send_mmc(C.uchar(0x7F), C.uchar(cmd))
}
