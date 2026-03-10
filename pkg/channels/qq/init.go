package qq

import (
	"github.com/sipeed/freeclaw/pkg/bus"
	"github.com/sipeed/freeclaw/pkg/channels"
	"github.com/sipeed/freeclaw/pkg/config"
)

func init() {
	channels.RegisterFactory("qq", func(cfg *config.Config, b *bus.MessageBus) (channels.Channel, error) {
		return NewQQChannel(cfg.Channels.QQ, b)
	})
}
