package telegram

import (
	"github.com/sipeed/freeclaw/pkg/bus"
	"github.com/sipeed/freeclaw/pkg/channels"
	"github.com/sipeed/freeclaw/pkg/config"
)

func init() {
	channels.RegisterFactory("telegram", func(cfg *config.Config, b *bus.MessageBus) (channels.Channel, error) {
		return NewTelegramChannel(cfg, b)
	})
}
