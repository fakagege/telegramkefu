package broadcast

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	"my-tg-bot/internal/cache"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// State constants for the broadcast builder
const (
	StateBroadcastAwaitText = iota + 10 // Use a higher start value to avoid conflicts
	StateBroadcastAwaitMedia
	StateBroadcastAwaitButtons
)

// Message defines the structure for a broadcast message.
type Message struct {
	Text    string
	MediaID string
	Type    string // "photo", "video", etc.
	Buttons tgbotapi.InlineKeyboardMarkup
}

// Manager handles all broadcast-related logic.
type Manager struct {
	API                       *tgbotapi.BotAPI
	RedisClient               *cache.RedisClient
	AdminStates               map[int64]int
	Broadcasts                map[int64]Message
	BroadcastPromptMessageIDs map[int64]int
}

// NewManager creates a new broadcast manager.
func NewManager(api *tgbotapi.BotAPI, redisClient *cache.RedisClient, adminStates map[int64]int) *Manager {
	return &Manager{
		API:                       api,
		RedisClient:               redisClient,
		AdminStates:               adminStates,
		Broadcasts:                make(map[int64]Message),
		BroadcastPromptMessageIDs: make(map[int64]int),
	}
}

// StartBroadcastBuilder initializes the broadcast creation process for an admin.
func (m *Manager) StartBroadcastBuilder(chatID int64) {
	log.Printf("开始广播构建，chatID: %d", chatID)
	m.Broadcasts[chatID] = Message{}
	m.AdminStates[chatID] = StateBroadcastAwaitText
	msg := tgbotapi.NewMessage(chatID, "请输入广播的文本内容，或点击下方按钮取消：")
	msg.ReplyMarkup = m.getCancelKeyboard()
	_, err := m.API.Send(msg)
	if err != nil {
		log.Printf("发送广播文本提示失败，chatID %d: %v", chatID, err)
	}
	log.Printf("设置状态为 StateBroadcastAwaitText，chatID: %d", chatID)
}

// HandleCallbackQuery processes callback queries related to the broadcast builder.
func (m *Manager) HandleCallbackQuery(q *tgbotapi.CallbackQuery) bool {
	if !strings.HasPrefix(q.Data, "bbuild_") {
		return false
	}

	log.Printf("处理广播回调，chatID %d，数据: %s", q.Message.Chat.ID, q.Data)
	callback := tgbotapi.NewCallback(q.ID, "")
	m.API.Request(callback)

	chatID := q.Message.Chat.ID
	action := q.Data

	switch action {
	case "bbuild_set_text":
		m.AdminStates[chatID] = StateBroadcastAwaitText
		msg := tgbotapi.NewMessage(chatID, "请输入广播的文本内容，或点击下方按钮取消：")
		msg.ReplyMarkup = m.getCancelKeyboard()
		_, err := m.API.Send(msg)
		if err != nil {
			log.Printf("发送文本设置提示失败，chatID %d: %v", chatID, err)
		}
		log.Printf("设置状态为 StateBroadcastAwaitText，chatID: %d", chatID)
	case "bbuild_set_media":
		m.AdminStates[chatID] = StateBroadcastAwaitMedia
		msg := tgbotapi.NewMessage(chatID, "请发送一张图片或一个视频作为广播的媒体内容，或点击下方按钮跳过：")
		msg.ReplyMarkup = m.getSkipMediaKeyboard()
		_, err := m.API.Send(msg)
		if err != nil {
			log.Printf("发送媒体设置提示失败，chatID %d: %v", chatID, err)
		}
		log.Printf("设置状态为 StateBroadcastAwaitMedia，chatID: %d", chatID)
	case "bbuild_skip_media":
		currentBroadcast := m.Broadcasts[chatID]
		currentBroadcast.MediaID = ""
		currentBroadcast.Type = ""
		m.Broadcasts[chatID] = currentBroadcast
		m.AdminStates[chatID] = StateBroadcastAwaitButtons
		callback := tgbotapi.NewCallback(q.ID, "✅ 已跳过媒体设置")
		m.API.Request(callback)
		msgText := "媒体已跳过！请输入广播的按钮，每行一个，格式为：\n`按钮文字 | 链接`\n\n例如：\n`关注频道 | https://t.me/channel`\n`靓号商城 | https://t.me/store`\n或点击下方按钮跳过（清除按钮）："
		msg := tgbotapi.NewMessage(chatID, msgText)
		msg.ParseMode = tgbotapi.ModeMarkdown
		msg.ReplyMarkup = m.getSkipButtonsKeyboard()
		_, err := m.API.Send(msg)
		if err != nil {
			log.Printf("发送按钮设置提示失败，chatID %d: %v", chatID, err)
		}
		log.Printf("媒体跳过，切换到 StateBroadcastAwaitButtons，chatID: %d", chatID)
	case "bbuild_set_buttons":
		m.AdminStates[chatID] = StateBroadcastAwaitButtons
		msgText := "请输入广播的按钮，每行一个，格式为：\n`按钮文字 | 链接`\n\n例如：\n`关注频道 | https://t.me/channel`\n`靓号商城 | https://t.me/store`\n或点击下方按钮跳过（清除按钮）："
		msg := tgbotapi.NewMessage(chatID, msgText)
		msg.ParseMode = tgbotapi.ModeMarkdown
		msg.ReplyMarkup = m.getSkipButtonsKeyboard()
		_, err := m.API.Send(msg)
		if err != nil {
			log.Printf("发送按钮设置提示失败，chatID %d: %v", chatID, err)
		}
		log.Printf("设置状态为 StateBroadcastAwaitButtons，chatID: %d", chatID)
	case "bbuild_skip_buttons":
		currentBroadcast := m.Broadcasts[chatID]
		currentBroadcast.Buttons = tgbotapi.NewInlineKeyboardMarkup()
		m.Broadcasts[chatID] = currentBroadcast
		m.AdminStates[chatID] = 0 // StateNone
		callback := tgbotapi.NewCallback(q.ID, "✅ 已跳过按钮设置")
		m.API.Request(callback)
		m.sendBroadcastBuilderMenu(chatID)
		log.Printf("按钮跳过，切换到 StateNone，chatID: %d", chatID)
	case "bbuild_preview":
		m.sendBroadcastPreview(chatID)
	case "bbuild_cancel":
		m.AdminStates[chatID] = 0 // StateNone
		delete(m.Broadcasts, chatID)
		delete(m.BroadcastPromptMessageIDs, chatID)
		deleteMsg := tgbotapi.NewDeleteMessage(chatID, q.Message.MessageID)
		m.API.Request(deleteMsg)
		msg := tgbotapi.NewMessage(chatID, "广播创建已取消。")
		m.API.Send(msg)
		log.Printf("广播创建已取消，chatID: %d", chatID)
	case "bbuild_send":
		m.executeBroadcast(chatID)
		m.AdminStates[chatID] = 0 // StateNone
		delete(m.Broadcasts, chatID)
		delete(m.BroadcastPromptMessageIDs, chatID)
		deleteMsg := tgbotapi.NewDeleteMessage(chatID, q.Message.MessageID)
		m.API.Request(deleteMsg)
		log.Printf("广播发送完成，chatID: %d", chatID)
	}
	return true
}

// HandleMessageInput processes messages from admins when they are in a broadcast-building state.
func (m *Manager) HandleMessageInput(msg *tgbotapi.Message) bool {
	chatID := msg.Chat.ID
	state, ok := m.AdminStates[chatID]
	if !ok {
		log.Printf("未找到广播状态，chatID %d", chatID)
		return false
	}

	log.Printf("处理广播消息，chatID %d，状态 %d，内容: %s", chatID, state, msg.Text)
	currentBroadcast := m.Broadcasts[chatID]

	switch state {
	case StateBroadcastAwaitText:
		if msg.Text == "" {
			log.Printf("无效的文本输入，chatID %d", chatID)
			errMsg := tgbotapi.NewMessage(chatID, "请输入有效的文本内容，或点击下方按钮取消。")
			errMsg.ReplyMarkup = m.getCancelKeyboard()
			m.API.Send(errMsg)
			return true
		}
		currentBroadcast.Text = msg.Text
		m.Broadcasts[chatID] = currentBroadcast
		m.AdminStates[chatID] = StateBroadcastAwaitMedia
		deleteUserMsg := tgbotapi.NewDeleteMessage(chatID, msg.MessageID)
		m.API.Request(deleteUserMsg)
		mediaPrompt := tgbotapi.NewMessage(chatID, "文本已设置！请发送一张图片或一个视频作为广播的媒体内容，或点击下方按钮跳过：")
		mediaPrompt.ReplyMarkup = m.getSkipMediaKeyboard()
		_, err := m.API.Send(mediaPrompt)
		if err != nil {
			log.Printf("发送媒体提示失败，chatID %d: %v", chatID, err)
		}
		log.Printf("文本设置完成，切换到 StateBroadcastAwaitMedia，chatID: %d", chatID)

	case StateBroadcastAwaitMedia:
		mediaID := ""
		mediaType := ""
		if len(msg.Photo) > 0 {
			mediaID = msg.Photo[len(msg.Photo)-1].FileID
			mediaType = "photo"
		} else if msg.Video != nil {
			mediaID = msg.Video.FileID
			mediaType = "video"
		} else {
			log.Printf("无效的媒体输入，chatID %d", chatID)
			errMsg := tgbotapi.NewMessage(chatID, "❌ 无效输入。请发送图片或视频，或点击下方按钮跳过。")
			errMsg.ReplyMarkup = m.getSkipMediaKeyboard()
			m.API.Send(errMsg)
			return true
		}
		currentBroadcast.MediaID = mediaID
		currentBroadcast.Type = mediaType
		m.Broadcasts[chatID] = currentBroadcast
		m.AdminStates[chatID] = StateBroadcastAwaitButtons
		deleteUserMsg := tgbotapi.NewDeleteMessage(chatID, msg.MessageID)
		m.API.Request(deleteUserMsg)
		buttonPrompt := tgbotapi.NewMessage(chatID, "媒体已设置！请输入广播的按钮，每行一个，格式为：\n`按钮文字 | 链接`\n\n例如：\n`关注频道 | https://t.me/channel`\n`靓号商城 | https://t.me/store`\n或点击下方按钮跳过（清除按钮）：")
		buttonPrompt.ParseMode = tgbotapi.ModeMarkdown
		buttonPrompt.ReplyMarkup = m.getSkipButtonsKeyboard()
		_, err := m.API.Send(buttonPrompt)
		if err != nil {
			log.Printf("发送按钮提示失败，chatID %d: %v", chatID, err)
		}
		log.Printf("媒体设置完成，切换到 StateBroadcastAwaitButtons，chatID: %d", chatID)

	case StateBroadcastAwaitButtons:
		lines := strings.Split(msg.Text, "\n")
		for i, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, "|", 2)
			if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
				log.Printf("无效按钮格式，chatID %d，第 %d 行: %s", chatID, i+1, line)
				errMsg := tgbotapi.NewMessage(chatID, fmt.Sprintf("第 %d 行格式错误：%s\n正确格式为：按钮文字 | 链接\n例如：关注频道 | https://t.me/channel", i+1, line))
				errMsg.ReplyMarkup = m.getSkipButtonsKeyboard()
				m.API.Send(errMsg)
				return true
			}
			url := strings.TrimSpace(parts[1])
			url = strings.Trim(url, "`")
			if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
				log.Printf("无效 URL，chatID %d，第 %d 行: %s", chatID, i+1, url)
				errMsg := tgbotapi.NewMessage(chatID, fmt.Sprintf("第 %d 行 URL 无效：%s\n请使用 http:// 或 https:// 开头的链接", i+1, url))
				errMsg.ReplyMarkup = m.getSkipButtonsKeyboard()
				m.API.Send(errMsg)
				return true
			}
		}
		currentBroadcast.Buttons = ParseButtons(msg.Text)
		m.Broadcasts[chatID] = currentBroadcast
		m.AdminStates[chatID] = 0 // StateNone
		deleteUserMsg := tgbotapi.NewDeleteMessage(chatID, msg.MessageID)
		m.API.Request(deleteUserMsg)
		m.sendBroadcastBuilderMenu(chatID)
		log.Printf("按钮设置完成，切换到 StateNone，chatID: %d", chatID)
	}
	return true
}

// getSkipMediaKeyboard 获取跳过媒体的键盘
func (m *Manager) getSkipMediaKeyboard() tgbotapi.InlineKeyboardMarkup {
	skipButton := tgbotapi.NewInlineKeyboardButtonData("⏭️ 跳过媒体", "bbuild_skip_media")
	row := tgbotapi.NewInlineKeyboardRow(skipButton)
	return tgbotapi.NewInlineKeyboardMarkup(row)
}

// getSkipButtonsKeyboard 获取跳过按钮的键盘
func (m *Manager) getSkipButtonsKeyboard() tgbotapi.InlineKeyboardMarkup {
	skipButton := tgbotapi.NewInlineKeyboardButtonData("⏭️ 跳过按钮", "bbuild_skip_buttons")
	row := tgbotapi.NewInlineKeyboardRow(skipButton)
	return tgbotapi.NewInlineKeyboardMarkup(row)
}

// getCancelKeyboard 获取取消的键盘
func (m *Manager) getCancelKeyboard() tgbotapi.InlineKeyboardMarkup {
	cancelButton := tgbotapi.NewInlineKeyboardButtonData("❌ 取消广播", "bbuild_cancel")
	row := tgbotapi.NewInlineKeyboardRow(cancelButton)
	return tgbotapi.NewInlineKeyboardMarkup(row)
}

func (m *Manager) sendBroadcastBuilderMenu(chatID int64) {
	broadcast := m.Broadcasts[chatID]
	text := "📢 **广播消息构建器**\n\n"
	text += "请确认你的广播消息内容：\n\n"
	text += "1️⃣ **文本内容:** "
	if broadcast.Text != "" {
		text += fmt.Sprintf("✅ %s\n", broadcast.Text)
	} else {
		text += "❌ (未设置)\n"
	}

	text += "2️⃣ **媒体内容 (图片/视频):** "
	if broadcast.MediaID != "" {
		text += fmt.Sprintf("✅ (%s 已设置)\n", broadcast.Type)
	} else {
		text += "❌ (未设置)\n"
	}

	text += "3️⃣ **按钮:** "
	if len(broadcast.Buttons.InlineKeyboard) > 0 {
		text += "✅ (已设置)\n"
	} else {
		text += "❌ (未设置)\n"
	}
	text += "\n"

	if broadcast.Text != "" || broadcast.MediaID != "" {
		text += "点击 **发送预览** 查看当前效果。\n"
		text += "点击 **确认发送** 将消息推送给所有用户。\n"
	} else {
		text += "请至少设置文本或媒体内容以继续。\n"
	}

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = tgbotapi.ModeMarkdown
	msg.ReplyMarkup = m.getBroadcastBuilderKeyboard(broadcast)

	if m.BroadcastPromptMessageIDs[chatID] != 0 {
		deleteMsg := tgbotapi.NewDeleteMessage(chatID, m.BroadcastPromptMessageIDs[chatID])
		m.API.Request(deleteMsg)
	}

	sentMsg, err := m.API.Send(msg)
	if err == nil {
		m.BroadcastPromptMessageIDs[chatID] = sentMsg.MessageID
	} else {
		log.Printf("发送广播构建菜单失败，chatID %d: %v", chatID, err)
	}
}

func (m *Manager) getBroadcastBuilderKeyboard(broadcast Message) tgbotapi.InlineKeyboardMarkup {
	var rows [][]tgbotapi.InlineKeyboardButton

	row1 := tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("1️⃣ 修改文本", "bbuild_set_text"),
		tgbotapi.NewInlineKeyboardButtonData("2️⃣ 修改媒体", "bbuild_set_media"),
	)
	row2 := tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("3️⃣ 修改按钮", "bbuild_set_buttons"),
	)
	rows = append(rows, row1, row2)

	if broadcast.Text != "" || broadcast.MediaID != "" {
		previewRow := tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("👀 发送预览", "bbuild_preview"),
		)
		rows = append(rows, previewRow)

		sendRow := tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🚀 确认发送", "bbuild_send"),
		)
		rows = append(rows, sendRow)
	}

	cancelRow := tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("❌ 取消", "bbuild_cancel"),
	)
	rows = append(rows, cancelRow)

	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

func (m *Manager) sendBroadcastPreview(chatID int64) {
	broadcast := m.Broadcasts[chatID]
	if broadcast.Text == "" && broadcast.MediaID == "" {
		msg := tgbotapi.NewMessage(chatID, "无法预览，广播内容为空。")
		m.API.Send(msg)
		log.Printf("广播预览失败，chatID %d：内容为空", chatID)
		return
	}

	previewMsg := tgbotapi.NewMessage(chatID, "--- 预览 ---")
	m.API.Send(previewMsg)
	m.sendComplexMessage(chatID, broadcast)
	log.Printf("发送广播预览，chatID: %d", chatID)
}

func (m *Manager) executeBroadcast(chatID int64) {
	broadcast := m.Broadcasts[chatID]
	if broadcast.Text == "" && broadcast.MediaID == "" {
		msg := tgbotapi.NewMessage(chatID, "无法发送，广播内容为空。")
		m.API.Send(msg)
		log.Printf("广播发送失败，chatID %d：内容为空", chatID)
		return
	}

	allUserIDsStr, err := m.RedisClient.GetAllUserIDs(context.Background(), "telegram_bot_users")
	if err != nil {
		log.Printf("获取所有用户ID失败，chatID %d: %v", chatID, err)
		msg := tgbotapi.NewMessage(chatID, "广播失败：无法获取用户列表。")
		m.API.Send(msg)
		return
	}

	go func() {
		count := 0
		for _, userIDStr := range allUserIDsStr {
			userID, _ := strconv.ParseInt(userIDStr, 10, 64)
			if userID != 0 {
				if m.sendComplexMessage(userID, broadcast) {
					count++
				}
			}
		}
		confirmMsg := tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ 广播发送完成，共成功发送给 %d 位用户。", count))
		m.API.Send(confirmMsg)
		log.Printf("广播发送完成，chatID %d，成功发送给 %d 位用户", chatID, count)
	}()
}

func (m *Manager) sendComplexMessage(chatID int64, broadcast Message) bool {
	var err error
	// 添加 📢 前缀到文本或媒体标题
	messageText := "📢 " + broadcast.Text

	if broadcast.MediaID != "" {
		var shareable tgbotapi.Chattable
		var markup *tgbotapi.InlineKeyboardMarkup
		if len(broadcast.Buttons.InlineKeyboard) > 0 {
			markup = &broadcast.Buttons
		}

		switch broadcast.Type {
		case "photo":
			photo := tgbotapi.NewPhoto(chatID, tgbotapi.FileID(broadcast.MediaID))
			photo.Caption = messageText
			photo.ReplyMarkup = markup
			shareable = photo
		case "video":
			video := tgbotapi.NewVideo(chatID, tgbotapi.FileID(broadcast.MediaID))
			video.Caption = messageText
			video.ReplyMarkup = markup
			shareable = video
		}
		if shareable != nil {
			_, err = m.API.Send(shareable)
		} else {
			err = fmt.Errorf("不支持的媒体类型: %s", broadcast.Type)
		}
	} else if broadcast.Text != "" {
		msg := tgbotapi.NewMessage(chatID, messageText)
		if len(broadcast.Buttons.InlineKeyboard) > 0 {
			msg.ReplyMarkup = broadcast.Buttons
		}
		_, err = m.API.Send(msg)
	}

	if err != nil {
		if strings.Contains(err.Error(), "bot was blocked by the user") {
			log.Printf("用户 %d 已屏蔽机器人，将从广播列表移除。", chatID)
		} else {
			log.Printf("发送消息给 %d 失败: %v", chatID, err)
		}
		return false
	}
	log.Printf("成功发送广播消息给 chatID %d，内容: %s", chatID, messageText)
	return true
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
