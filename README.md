# joy2midi-go

Usa um joystick/gamepad Linux como controlador MIDI, enviando os eventos
como uma porta ALSA sequencer (aparece no `aconnect -l`, QjackCtl, Carla,
e em qualquer coisa que já esteja recebendo MIDI via ALSA/PipeWire-JACK).

Sem cgo pesado nem libs de terceiros além do próprio ALSA — só
`libasound2-dev` (que provavelmente você já tem, dado seu setup com
PipeWire-JACK/Ardour).

## Build

```sh
sudo apt install golang-go libasound2-dev gcc   # se ainda não tiver
go build -o joy2midi-go .
```

## Descobrindo os índices do seu controle

Antes de configurar o mapeamento, descubra os índices reais de eixos e
botões do SEU joystick (variam de modelo pra modelo):

```sh
sudo apt install joystick   # fornece o jstest
jstest /dev/input/js0
```

Mexa cada analógico e aperte cada botão observando qual índice muda.

## Configuração

Edite um arquivo `.cfg` (veja `exemplo.cfg` incluído). Formato:

```
channel  = 1       # canal MIDI 1-16
deadzone = 2000     # zona morta em torno do centro do analógico (0-32767)

axis 0   => bend           # pitch bend
axis 1   => control 1      # CC1 (mod wheel)
axis 2   => ignore
button 0 => note 60        # dó central
button 4 => control 64     # sustain on/off
```

Alvos possíveis: `note N`, `control N`, `bend` (só eixo), `ignore`.

Pra `control N` de um eixo, dá pra escolher como o centro/extremos são
escalados (útil pra CCs onde o repouso deve ser 0, tipo mod wheel):

```
axis 1 => control 1 bipolar   # padrão: centro=64, extremos=0/127 (bom pra pan)
axis 1 => control 1 up        # centro=0, só o lado positivo sobe até 127
axis 1 => control 1 down      # centro=0, só o lado negativo sobe até 127
axis 1 => control 1 abs       # centro=0, QUALQUER lado sobe até 127
```

Se `up`/`down` saírem invertidos na prática, é só trocar a palavra-chave —
a polaridade do eixo varia de driver pra driver.

### Controle de transporte (MMC)

Além de note/control/bend, dá pra mapear botão ou eixo pra um comando
**MMC** (MIDI Machine Control) -- comandos de transporte padronizados que
o Ardour entende nativamente, sem precisar de MIDI Learn:

```
button 0 => mmc play
button 1 => mmc stop
axis 3    => mmc ffwd rewind    # eixo bidirecional: positivo=ffwd, negativo=rewind
```

Comandos disponíveis: `play`, `stop`, `deferred_play`, `ffwd`, `rewind`,
`record` (punch in), `record_exit` (punch out), `record_pause`, `pause`,
`eject`.

Em botão, o comando dispara uma vez ao pressionar (não faz nada ao
soltar). Em eixo, dispara ao cruzar a zona morta pra um lado ou pro
outro, e não repete enquanto o eixo continua deslocado (só na borda).

**Pro Ardour receber:** Edit > Preferences > Transport > Chase > marque
"Respond to MMC commands" (o "Inbound MMC device ID" padrão de 127 já
bate com o que este programa envia). Depois conecte a porta ALSA deste
programa na entrada `ardour:MMC in`, em Window > MIDI Connections.

Pra ações que não têm equivalente MMC (marcadores, loop, etc.), mapeie o
botão como `control N` normal e faça MIDI Learn direto no Ardour
(clique direito no controle desejado > MIDI Learn > aperte o botão).

### Ações do editor (zoom, playhead, etc.) via `pulse`

Pra eixos digitais tipo o hat de um gamepad, `pulse` dispara um CC=127
uma vez ao cruzar pra cada lado (e CC=0 ao voltar pro centro) — pensado
pra carregar o gatilho de uma ação do editor via **binding map** do
Ardour (não é MIDI Learn direto; ações de menu em geral só são
bindáveis via um arquivo XML):

```
axis 3 => pulse 102 103   # positivo=CC102, negativo=CC103
```

No binding map XML (carregado em Edit > Preferences > Control Surfaces
> Generic MIDI), você associa cada CC a uma ação:

```xml
<ArdourMIDIBindings version="1.0.0" name="joy2midi-go" manufacturer="fellipec">
  <Binding channel="1" ctl="102" value="127" action="Editor/scroll-playhead-forward"/>
  <Binding channel="1" ctl="103" value="127" action="Editor/scroll-playhead-backward"/>
  <Binding channel="1" ctl="104" value="127" action="Editor/temporal-zoom-in"/>
  <Binding channel="1" ctl="105" value="127" action="Editor/temporal-zoom-out"/>
</ArdourMIDIBindings>
```

Pra descobrir o nome exato de uma ação que não sabe (tipo "voltar pro
início" ou "toggle do metrônomo"), rode `ardour -A` no terminal — ele
lista todos os action-names disponíveis na sua versão instalada.

### Dividir um eixo em duas CCs contínuas com `split`

Diferente do `up`/`down` de `control` (que trava metade do curso em
0), `split` usa o eixo inteiro: cada metade manda um CC **diferente**,
proporcional à distância do centro:

```
axis 1 => split 1 11   # empurra = CC1 (mod), puxa = CC11 (expression)
```

Bom pra aproveitar as duas metades de um eixo com destinos diferentes
(tipo o "JS+/JS-" assinalável separadamente em teclados Yamaha/Korg),
em vez de desperdiçar metade do curso.

## Rodando

```sh
./joy2midi-go -config exemplo.cfg -device /dev/input/js0 -name meu-joystick -v
```

A flag `-v` liga o log de cada evento (útil pra depurar o mapeamento).
Ctrl+C encerra de forma limpa.

Depois, conecte a porta ALSA criada (nome dado em `-name`) ao Ardour ou a
qualquer sintetizador, via `aconnect`, QjackCtl ou Carla — do mesmo jeito
que você já roteia o P-45.

```sh
aconnect -l                    # lista clientes/portas MIDI
aconnect 'meu-joystick' 'Ardour'   # conecta (nomes reais podem variar)
```

## Limitações conhecidas / próximos passos

- Suporta um joystick por instância (`-device`); rode duas instâncias com
  `-name` diferentes se quiser dois controles ao mesmo tempo.
- Botões só mandam note on/off "hard" (velocity fixa 127). Dá pra
  adicionar velocity variável lendo pressão analógica de gatilhos, se seu
  pad expuser isso como eixo.
- Sem suporte a rumble/force feedback (não é o objetivo aqui).
- Testado só a nível de compilação e parsing de config no ambiente do
  Claude, que não tem device de joystick real — teste com hardware antes
  de confiar de olhos fechados.
