package feishu

import (
	"github.com/sipeed/freeclaw/pkg/bus"
	"github.com/sipeed/freeclaw/pkg/channels"
	"github.com/sipeed/freeclaw/pkg/config"
)

func init() {
	channels.RegisterFactory("feishu", func(cfg *config.Config, b *bus.MessageBus) (channels.Channel, error) {
		return NewFeishuChannel(cfg.Channels.Feishu, b)
	})
}
