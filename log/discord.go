package log

import (
	"github.com/gtuk/discordwebhook"
)

type webhook struct {
	Username string
	Url      string
}

var DiscordWebhook = &webhook{}

func InitDiscordLogger(url string) {
	if url != "" {
		DiscordWebhook.Url = url
		DiscordWebhook.Username = "ASMR Downloader"
	}
}

func (DW *webhook) Send(message string) error {
	if DW.Url == "" {
		return nil // 如果没有设置URL，则不发送消息
	}
	return discordwebhook.SendMessage(DW.Url, discordwebhook.Message{
		Username: &DW.Username,
		Content:  &message,
	})
}
