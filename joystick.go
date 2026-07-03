package main

import (
	"encoding/binary"
	"fmt"
	"os"
)

// jsEventType flags, ver linux/joystick.h
const (
	jsEventButton = 0x01
	jsEventAxis   = 0x02
	jsEventInit   = 0x80 // OR'ed com o tipo no primeiro estado reportado de cada entrada
)

// jsEvent espelha exatamente o struct js_event do kernel (8 bytes, little-endian):
//
//	struct js_event {
//	    __u32 time;   // timestamp em ms
//	    __s16 value;  // valor do eixo ou 0/1 do botão
//	    __u8  type;   // JS_EVENT_BUTTON | JS_EVENT_AXIS | JS_EVENT_INIT
//	    __u8  number; // índice do eixo/botão
//	};
type jsEvent struct {
	Time   uint32
	Value  int16
	Type   uint8
	Number uint8
}

// Joystick representa um dispositivo /dev/input/jsX aberto.
type Joystick struct {
	f *os.File
}

func openJoystick(path string) (*Joystick, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("abrindo %s: %w", path, err)
	}
	return &Joystick{f: f}, nil
}

func (j *Joystick) Close() error {
	return j.f.Close()
}

// ReadEvent bloqueia até o próximo evento de eixo/botão.
func (j *Joystick) ReadEvent() (jsEvent, error) {
	var ev jsEvent
	err := binary.Read(j.f, binary.LittleEndian, &ev)
	return ev, err
}

// IsInit indica se este é o evento sintético de estado inicial
// (enviado pelo kernel para cada eixo/botão ao abrir o device).
func (ev jsEvent) IsInit() bool {
	return ev.Type&jsEventInit != 0
}

// IsAxis / IsButton ignoram o bit de init ao checar o tipo.
func (ev jsEvent) IsAxis() bool {
	return ev.Type&^jsEventInit == jsEventAxis
}

func (ev jsEvent) IsButton() bool {
	return ev.Type&^jsEventInit == jsEventButton
}
