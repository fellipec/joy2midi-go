# joy2midi-go

Turn a Linux joystick/gamepad into a MIDI controller. It exposes itself as
an ALSA sequencer port (shows up in `aconnect -l`, QjackCtl, Carla, or
anything already listening for MIDI via ALSA/PipeWire-JACK), and can send
notes, CCs, pitch bend, MMC transport commands, and Ardour editor actions
(zoom, playhead nudge, panic, etc.) — all driven by a plain-text config
file, no rebuild required to change your mapping.

No heavy cgo dependencies beyond ALSA itself — just `libasound2-dev`
(you probably already have it if you're running PipeWire-JACK/Ardour).

> **Note:** this project was built collaboratively with
> [Claude](https://claude.ai) (Anthropic), pair-programming through the
> design, implementation, and debugging in a chat session — including
> compiling and testing the code in a sandboxed environment before
> handing it over. All of the Ardour-specific integration details
> (MMC, Generic MIDI binding maps, action names) were verified against
> Ardour's own documentation and action list rather than assumed.

## Build

```sh
sudo apt install golang-go libasound2-dev gcc   # if you don't have them yet
go build -o joy2midi-go .
```

## Finding your controller's axis/button indices

Before writing a mapping, find the real axis and button indices for
YOUR joystick (they vary between models):

```sh
sudo apt install joystick   # provides jstest
jstest /dev/input/js0
```

Move each analog stick and press each button, watching which index
changes.

## Configuration

Edit a `.cfg` file (see the included `exemplo.cfg`). Format:

```
channel  = 1        # MIDI channel 1-16
deadzone = 2000      # dead zone around analog center (0-32767)

axis 0   => bend            # pitch bend
axis 1   => control 1       # CC1 (mod wheel)
axis 2   => ignore
button 0 => note 60         # middle C
button 4 => control 64      # sustain on/off
```

Available targets: `note N`, `control N`, `bend` (axis only), `mmc`,
`pulse`, `split`, `ignore`.

### Scaling a single CC (`control`)

For an axis mapped to `control N`, you can choose how center/extremes
are scaled (useful for CCs that should rest at 0, like a mod wheel):

```
axis 1 => control 1 bipolar   # default: center=64, extremes=0/127 (good for pan)
axis 1 => control 1 up        # center=0, only the positive side rises to 127
axis 1 => control 1 down      # center=0, only the negative side rises to 127
axis 1 => control 1 abs       # center=0, EITHER side rises to 127
```

If `up`/`down` end up inverted in practice, just swap the keyword —
axis polarity varies by driver/hardware.

### Transport control (MMC)

Besides note/control/bend, you can map a button or axis to an **MMC**
(MIDI Machine Control) command — standardized transport commands that
Ardour understands natively, no MIDI Learn required:

```
button 0 => mmc play
button 1 => mmc stop
axis 3    => mmc ffwd rewind    # bidirectional axis: positive=ffwd, negative=rewind
```

Available commands: `play`, `stop`, `deferred_play`, `ffwd`, `rewind`,
`record` (punch in), `record_exit` (punch out), `record_pause`,
`pause`, `eject`.

On a button, the command fires once on press (nothing on release). On
an axis, it fires when crossing the dead zone to either side, and
won't repeat while the axis stays deflected (edge-triggered only).

**For Ardour to receive it:** Edit > Preferences > Transport > Chase >
check "Respond to MMC commands" (the default "Inbound MMC device ID"
of 127 already matches what this program sends). Then connect this
program's ALSA port to the `ardour:MMC in` input, in Window > MIDI
Connections.

For actions with no MMC equivalent (markers, loop, etc.), map the
button as a plain `control N` and either MIDI Learn it directly in
Ardour, or use a binding map (see below).

### Editor actions (zoom, playhead, etc.) via `pulse`

For digital axes like a gamepad's hat switch, `pulse` fires a CC=127
once when crossing to each side (and CC=0 when returning to center) —
meant to carry the trigger for an Ardour editor action via a **binding
map** (this is not direct MIDI Learn; most menu actions can only be
bound through an XML file):

```
axis 3 => pulse 102 103   # positive=CC102, negative=CC103
```

In the binding map XML (loaded via Edit > Preferences > Control
Surfaces > Generic MIDI), you tie each CC to an action:

```xml
<ArdourMIDIBindings version="1.0.0" name="joy2midi-go" manufacturer="fellipec">
  <Binding channel="1" ctl="102" value="127" action="Editor/scroll-playhead-forward"/>
  <Binding channel="1" ctl="103" value="127" action="Editor/scroll-playhead-backward"/>
  <Binding channel="1" ctl="104" value="127" action="EditorEditing/temporal-zoom-in"/>
  <Binding channel="1" ctl="105" value="127" action="EditorEditing/temporal-zoom-out"/>
</ArdourMIDIBindings>
```

To find the exact name of an action you don't know, run `ardour -A` in
a terminal — it lists every action-name available in your installed
version. Action namespaces aren't always what you'd guess (e.g. zoom
lives under `EditorEditing/`, not `Editor/`) — always verify against
the real list rather than assuming.

### Continuous mixer controls (faders, gain) via `uri`

Binding-map `action=` entries are for one-shot triggers (menu items).
For a **continuous** control like a track or bus fader, Ardour has a
separate `uri=` control-address scheme in the same binding map file —
no `value=` attribute, since it needs to track the full 0-127 range,
not fire on one specific value:

```xml
<Binding channel="1" ctl="20" uri="/bus/gain master"/>
<Binding channel="1" ctl="21" uri="/route/gain 1"/>
```

`/bus/gain master` targets the Master bus fader; `/route/gain N`
targets the fader of the track/bus with remote-control ID `N`. Pair
this with a plain `control N` (no `up`/`down`/`abs` needed — a
fader/throttle lever doesn't rest at center) in your `.cfg`.

If you'd rather not deal with binding maps at all, the same result
can be reached with **live MIDI Learn**: right-click won't do it —
it's **Ctrl + middle-click** on the fader, then move your control.

### Splitting one axis into two continuous CCs with `split`

Unlike `control`'s `up`/`down` (which clamps half the travel to 0),
`split` uses the entire axis: each half sends a **different** CC,
proportional to the distance from center:

```
axis 1 => split 1 11   # push = CC1 (mod), pull = CC11 (expression)
```

Good for putting two different destinations on the two halves of one
axis (like the separately-assignable "JS+/JS-" found on Yamaha/Korg
keyboards), instead of wasting half the travel.

> **Watch out for CC semantics:** not every CC rests at 0. Expression
> (CC11), for instance, is defined to rest at 127 (full volume) and
> *decrease* as it approaches 0 — the opposite convention from Mod
> Wheel (CC1), which rests at 0 and increases. `split`/`control`
> always treat center as 0 for both halves, so pairing them with a
> "rests-at-127" CC will make your instrument sound quiet near center
> and only recover as you push toward the CC's high end. Stick to CCs
> that follow the same "0 = neutral, 127 = max effect" convention
> (Mod Wheel, most Sound Controllers CC70-79) unless you know what
> you're doing.

## Running

```sh
./joy2midi-go -config exemplo.cfg -device /dev/input/js0 -name my-joystick -v
```

The `-v` flag logs every event (useful for debugging your mapping).
Ctrl+C shuts down cleanly.

Then connect the ALSA port it creates (named via `-name`) to Ardour or
any synth, using `aconnect`, QjackCtl, or Carla:

```sh
aconnect -l                         # list MIDI clients/ports
aconnect 'my-joystick' 'Ardour'     # connect (real names may vary)
```

A single output port can fan out to multiple destinations at once —
e.g. connect it to both `ardour:MMC in` (for transport) and your
synth's MIDI input (for bend/mod/expression). Non-note messages (CC,
SysEx) are simply ignored by destinations that don't understand them,
so this is safe.

## Known limitations / possible next steps

- Supports one joystick per instance (`-device`); run two instances
  with different `-name`s if you want two controllers at once.
- Buttons only send "hard" note on/off (fixed velocity 127). Variable
  velocity from analog trigger pressure could be added if your pad
  exposes that as an axis.
- No rumble/force-feedback support (out of scope for this project).
- Ardour's Generic MIDI fader response has a "Smoothing" setting
  (Preferences > Control Surfaces > Generic MIDI settings) that
  ignores large jumps in incoming CC value by default — set it to 127
  if a fast-moving axis (like a throttle) doesn't seem to track.