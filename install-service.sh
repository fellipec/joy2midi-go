#!/usr/bin/env bash
# install-service.sh -- instala o joy2midi-go como serviço systemd de usuário.
#
# Uso:
#   ./install-service.sh /dev/input/by-id/usb-SEU-JOYSTICK-aqui
#
# (rode `ls -la /dev/input/by-id/ | grep -i joystick` primeiro pra achar
#  o caminho certo; se não existir link em by-id/, passe /dev/input/jsN
#  mesmo, sabendo que o índice pode mudar entre boots)

set -euo pipefail

if [ $# -ne 1 ]; then
    echo "uso: $0 /dev/input/by-id/SEU-JOYSTICK" >&2
    exit 1
fi

DEVICE_PATH="$1"

if [ ! -e "$DEVICE_PATH" ]; then
    echo "aviso: $DEVICE_PATH não existe agora -- ok se o joystick estiver desplugado," >&2
    echo "        mas confirme que o caminho está certo antes de prosseguir." >&2
fi

DEVICE_UNIT="$(systemd-escape --path --suffix=device "$DEVICE_PATH")"
echo "Device unit resolvida: $DEVICE_UNIT"

mkdir -p "$HOME/.local/bin" "$HOME/.config/joy2midi-go" "$HOME/.config/systemd/user"

# build (assume que este script roda de dentro do diretório do projeto)
go build -o "$HOME/.local/bin/joy2midi-go" .
cp meu-joystick.cfg "$HOME/.config/joy2midi-go/"

# IMPORTANTE: NÃO use sed aqui pra substituir DEVICE_UNIT -- o valor tem
# sequências tipo "\x2d" (escape do systemd-escape), e o sed interpreta
# isso como escape hexadecimal na substituição, corrompendo o nome de
# volta pra "-" sem avisar nada. Usamos heredoc do bash em vez disso,
# que não reinterpreta o conteúdo da variável.
cat > "$HOME/.config/systemd/user/joy2midi-go.service" <<UNITEOF
[Unit]
Description=joy2midi-go: joystick-to-MIDI controller for Ardour
BindsTo=${DEVICE_UNIT}
After=${DEVICE_UNIT}
After=pipewire.service wireplumber.service

[Service]
Type=simple
ExecStart=%h/.local/bin/joy2midi-go -config %h/.config/joy2midi-go/meu-joystick.cfg -device ${DEVICE_PATH} -name meu-joystick
Restart=on-failure
RestartSec=3

[Install]
WantedBy=default.target
UNITEOF

systemctl --user daemon-reload
systemctl --user enable --now joy2midi-go.service

echo
echo "Feito. Status:"
systemctl --user status joy2midi-go.service --no-pager || true
echo
echo "Se quiser que rode mesmo sem sessão gráfica ativa (ex: antes do login):"
echo "  loginctl enable-linger \$USER"
