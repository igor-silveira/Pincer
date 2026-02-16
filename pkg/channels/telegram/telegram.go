package telegram

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"os"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/igorsilveira/pincer/pkg/channels"
	"github.com/igorsilveira/pincer/pkg/telemetry"
)

type Adapter struct {
	token    string
	bot      *bot.Bot
	inbound  chan channels.InboundMessage
	sessions *channels.SessionMap[int64]
}

func New(token string) (*Adapter, error) {
	if token == "" {
		token = os.Getenv("TELEGRAM_BOT_TOKEN")
	}
	if token == "" {
		return nil, fmt.Errorf("telegram: bot token not set")
	}
	return &Adapter{
		token:    token,
		inbound:  make(chan channels.InboundMessage, 256),
		sessions: channels.NewSessionMap[int64]("tg", func(k int64) string { return fmt.Sprintf("%d", k) }),
	}, nil
}

func (a *Adapter) Name() string { return "telegram" }

func (a *Adapter) Start(ctx context.Context) error {
	logger := telemetry.FromContext(ctx)

	opts := []bot.Option{
		bot.WithDefaultHandler(a.handleUpdate),
		bot.WithCallbackQueryDataHandler("approval:", bot.MatchTypePrefix, a.handleCallbackQuery),
	}

	b, err := bot.New(a.token, opts...)
	if err != nil {
		return fmt.Errorf("telegram: creating bot: %w", err)
	}
	a.bot = b

	logger.Info("telegram adapter started")

	go b.Start(ctx)
	return nil
}

func (a *Adapter) Stop(ctx context.Context) error {

	return nil
}

func (a *Adapter) Send(ctx context.Context, msg channels.OutboundMessage) error {
	chatID, ok := a.sessions.Reverse(msg.SessionID)
	if !ok {
		return fmt.Errorf("telegram: no chat for session %s", msg.SessionID)
	}

	chunks := channels.SplitMessage(msg.Content, 4096)
	for _, chunk := range chunks {
		if _, err := a.bot.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   chunk,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (a *Adapter) Receive() <-chan channels.InboundMessage {
	return a.inbound
}

func (a *Adapter) Capabilities() channels.ChannelCaps {
	return channels.ChannelCaps{
		SupportsStreaming: false,
		SupportsMedia:     true,
		SupportsReactions: true,
	}
}

func (a *Adapter) SendApprovalRequest(ctx context.Context, req channels.ApprovalRequest) error {
	chatID, ok := a.sessions.Reverse(req.SessionID)
	if !ok {
		return fmt.Errorf("telegram: no chat for session %s", req.SessionID)
	}

	text := fmt.Sprintf("🔧 <b>Tool approval needed: %s</b>\nInput: <code>%s</code>",
		html.EscapeString(req.ToolName), html.EscapeString(req.Input))

	keyboard := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: "✅ Approve", CallbackData: "approval:approve:" + req.RequestID},
				{Text: "❌ Deny", CallbackData: "approval:deny:" + req.RequestID},
			},
		},
	}

	_, err := a.bot.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   models.ParseModeHTML,
		ReplyMarkup: keyboard,
	})
	return err
}

func (a *Adapter) handleCallbackQuery(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.CallbackQuery == nil {
		return
	}

	data := update.CallbackQuery.Data
	parts := strings.SplitN(data, ":", 3)
	if len(parts) != 3 || parts[0] != "approval" {
		return
	}

	action := parts[1]
	requestID := parts[2]

	approved := action == "approve"

	_, _ = b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: update.CallbackQuery.ID,
		Text:            fmt.Sprintf("Tool %sd", action),
	})

	if update.CallbackQuery.Message.Message != nil {
		chatID := update.CallbackQuery.Message.Message.Chat.ID
		messageID := update.CallbackQuery.Message.Message.ID

		status := "✅ Approved"
		if !approved {
			status = "❌ Denied"
		}

		_, _ = b.EditMessageReplyMarkup(ctx, &bot.EditMessageReplyMarkupParams{
			ChatID:      chatID,
			MessageID:   messageID,
			ReplyMarkup: nil,
		})
		_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    chatID,
			MessageID: messageID,
			Text:      update.CallbackQuery.Message.Message.Text + "\n\n" + status,
		})
	}

	sessionID, _ := a.sessions.Lookup(update.CallbackQuery.Message.Message.Chat.ID)
	peerID := fmt.Sprintf("%d", update.CallbackQuery.From.ID)

	a.inbound <- channels.InboundMessage{
		ChannelName: "telegram",
		SessionID:   sessionID,
		PeerID:      peerID,
		ApprovalResponse: &channels.InboundApprovalResponse{
			RequestID: requestID,
			Approved:  approved,
		},
	}
}

func (a *Adapter) handleUpdate(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.Message == nil || update.Message.Text == "" {
		return
	}

	chatID := update.Message.Chat.ID
	peerID := fmt.Sprintf("%d", update.Message.From.ID)

	sessionID := a.sessions.GetOrCreate(chatID)

	slog.Debug("telegram message received",
		slog.Int64("chat_id", chatID),
		slog.String("peer_id", peerID),
	)

	a.inbound <- channels.InboundMessage{
		ChannelName: "telegram",
		SessionID:   sessionID,
		PeerID:      peerID,
		Content:     update.Message.Text,
	}
}

