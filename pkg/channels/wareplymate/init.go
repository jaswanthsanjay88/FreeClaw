package wareplymate

import (
	"github.com/sipeed/freeclaw/pkg/bus"
	"github.com/sipeed/freeclaw/pkg/channels"
	"github.com/sipeed/freeclaw/pkg/config"
)

func init() {
	channels.RegisterFactory("wareplymate", func(cfg *config.Config, b *bus.MessageBus) (channels.Channel, error) {
		return NewWAReplyMateChannel(cfg.Channels.WAReplyMate, b)
	})
}
