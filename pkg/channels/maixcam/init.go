package maixcam

import (
	"github.com/sipeed/freeclaw/pkg/bus"
	"github.com/sipeed/freeclaw/pkg/channels"
	"github.com/sipeed/freeclaw/pkg/config"
)

func init() {
	channels.RegisterFactory("maixcam", func(cfg *config.Config, b *bus.MessageBus) (channels.Channel, error) {
		return NewMaixCamChannel(cfg.Channels.MaixCam, b)
	})
}
