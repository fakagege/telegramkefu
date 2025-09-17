package welcome

import (
	"context"
	"fmt"
	"strings"

	"my-tg-bot/internal/cache"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// State constants for the welcome message editor
const (
	StateAwaitingWelcomeMessage = iota + 20 // Use a higher start value to avoid conflicts
	StateAwaitingWelcomeButtons
)

const (
	ConfigWelcomeMessage = "config:welcome_message"
	ConfigWelcomeButtons = "config:welcome_buttons"
)

// Manager handles all welcome-message-related logic.
type Manager struct {
	API         *tgbotapi.BotAPI
	RedisClient *cache.RedisClient
	AdminStates map[int64]int
}

// NewManager creates a new welcome message manager.
func NewManager(api *tgbotapi.BotAPI, redisClient *cache.RedisClient, adminStates map[int64]int) *Manager {
	return &Manager{
		API:         api,
		RedisClient: redisClient,
		AdminStates: adminStates,
	}
}

// HandleStartCommand sends the welcome message to a user.
func (m *Manager) HandleStartCommand(chatID int64) {
	welcomeMsgText, err := m.RedisClient.GetConfigValue(context.Background(), ConfigWelcomeMessage)
	if err != nil || welcomeMsgText == "" {
		welcomeMsgText = "👋 欢迎光临，我是私信小助手。直接在这里发消息，技术会回复。"
	}

	buttonsStr, err := m.RedisClient.GetConfigValue(context.Background(), ConfigWelcomeButtons)
	var keyboard tgbotapi.InlineKeyboardMarkup
	if err == nil && buttonsStr != "" {
		keyboard = ParseButtons(buttonsStr)
	}

	msg := tgbotapi.NewMessage(chatID, welcomeMsgText)
	if len(keyboard.InlineKeyboard) > 0 {
		msg.ReplyMarkup = keyboard
	}
	m.API.Send(msg)
}

// StartSetWelcomeProcess begins the process for an admin to set the welcome message.
func (m *Manager) StartSetWelcomeProcess(chatID int64) {
	// 先获取并显示当前欢迎语
	currentMsg, err := m.RedisClient.GetConfigValue(context.Background(), ConfigWelcomeMessage)
	if err != nil {
		currentMsg = "（无法获取当前欢迎语）"
	} else if currentMsg == "" {
		currentMsg = "（当前无欢迎语）"
	}
	displayMsg := tgbotapi.NewMessage(chatID, fmt.Sprintf("当前欢迎语：\n%s\n\n请输入新的欢迎语文本（可基于当前内容修改）：", currentMsg))
	m.API.Send(displayMsg)

	m.AdminStates[chatID] = StateAwaitingWelcomeMessage
}

// StartSetButtonsProcess begins the process for an admin to set the welcome buttons.
func (m *Manager) StartSetButtonsProcess(chatID int64) {
	// 先获取并显示当前按钮
	currentButtons, err := m.RedisClient.GetConfigValue(context.Background(), ConfigWelcomeButtons)
	if err != nil {
		currentButtons = "（无法获取当前按钮）"
	} else if currentButtons == "" {
		currentButtons = "（当前无按钮）"
	}
	msgText := fmt.Sprintf("当前欢迎按钮：\n%s\n\n请输入新的欢迎按钮，每行一个，格式为：\n`按钮文字 | 链接`\n\n例如：\n`关注频道 | https://t.me/channel`\n`靓号商城 | https://t.me/store`\n（可基于当前内容修改）", currentButtons)
	msg := tgbotapi.NewMessage(chatID, msgText)
	msg.ParseMode = tgbotapi.ModeMarkdown
	m.API.Send(msg)

	m.AdminStates[chatID] = StateAwaitingWelcomeButtons
}

// HandleAdminMessageInput processes messages from admins when they are in a welcome-editing state.
func (m *Manager) HandleAdminMessageInput(msg *tgbotapi.Message) bool {
	state, ok := m.AdminStates[msg.From.ID]
	if !ok {
		return false
	}

	switch state {
	case StateAwaitingWelcomeMessage:
		m.handleWelcomeMessageInput(msg)
		return true
	case StateAwaitingWelcomeButtons:
		m.handleWelcomeButtonsInput(msg)
		return true
	}
	return false
}

func (m *Manager) handleWelcomeMessageInput(msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	err := m.RedisClient.SetConfigValue(context.Background(), ConfigWelcomeMessage, msg.Text)
	if err != nil {
		errMsg := tgbotapi.NewMessage(chatID, fmt.Sprintf("保存欢迎语失败: %v", err))
		m.API.Send(errMsg)
		return
	}
	m.AdminStates[chatID] = 0 // StateNone
	reply := tgbotapi.NewMessage(chatID, "✅ 欢迎语已更新。")
	m.API.Send(reply)
	m.HandleStartCommand(chatID)
}

func (m *Manager) handleWelcomeButtonsInput(msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	err := m.RedisClient.SetConfigValue(context.Background(), ConfigWelcomeButtons, msg.Text)
	if err != nil {
		errMsg := tgbotapi.NewMessage(chatID, fmt.Sprintf("保存按钮失败: %v", err))
		m.API.Send(errMsg)
		return
	}
	m.AdminStates[chatID] = 0 // StateNone
	reply := tgbotapi.NewMessage(chatID, "✅ 欢迎按钮已更新。")
	m.API.Send(reply)
	m.HandleStartCommand(chatID)
}

// ParseButtons is a helper function to parse button data from a string.
func ParseButtons(data string) tgbotapi.InlineKeyboardMarkup {
	lines := strings.Split(data, "\n")
	var buttons []tgbotapi.InlineKeyboardButton
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) == 2 {
			text := strings.TrimSpace(parts[0])
			url := strings.TrimSpace(parts[1])
			url = strings.Trim(url, "`")
			buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonURL(text, url))
		}
	}

	var rows [][]tgbotapi.InlineKeyboardButton
	for i := 0; i < len(buttons); i += 2 {
		if i+1 < len(buttons) {
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(buttons[i], buttons[i+1]))
		} else {
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(buttons[i]))
		}
	}

	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}
