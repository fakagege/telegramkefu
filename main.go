package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"

	"my-tg-bot/internal/broadcast"
	"my-tg-bot/internal/cache"
	"my-tg-bot/internal/welcome"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
)

const (
	StateNone    = 0
	UsersPerPage = 10
)

// BotInstance 结构体保持不变
type BotInstance struct {
	API              *tgbotapi.BotAPI
	adminIDs         map[int64]bool
	adminStates      map[int64]int
	forwardToAdminID int64
	redisClient      *cache.RedisClient
	broadcastManager *broadcast.Manager
	welcomeManager   *welcome.Manager
}

// NewBotInstance 函数，添加日志以验证管理员 ID 和 Redis 连接
func NewBotInstance() (*BotInstance, error) {
	err := godotenv.Load()
	if err != nil {
		log.Println("警告：无法加载 .env 文件，将依赖环境变量。")
	}

	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("请设置 TELEGRAM_BOT_TOKEN 环境变量")
	}

	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, err
	}

	api.Debug = false
	log.Printf("机器人账号 %s", api.Self.UserName)

	redisAddr := os.Getenv("REDIS_ADDR")
	redisPassword := os.Getenv("REDIS_PASSWORD")
	redisDBStr := os.Getenv("REDIS_DB")
	redisDB, _ := strconv.Atoi(redisDBStr)
	redisClient, err := cache.NewRedisClient(redisAddr, redisPassword, redisDB)
	if err != nil {
		return nil, fmt.Errorf("无法连接到 Redis: %w", err)
	}
	log.Printf("成功连接到 Redis，地址: %s, 数据库: %d", redisAddr, redisDB)

	adminIDs := make(map[int64]bool)
	adminIDStr := os.Getenv("ADMIN_IDS")
	if adminIDStr != "" {
		ids := strings.Split(adminIDStr, ",")
		for _, idStr := range ids {
			id, err := strconv.ParseInt(strings.TrimSpace(idStr), 10, 64)
			if err == nil {
				adminIDs[id] = true
			}
		}
		log.Printf("加载的管理员 ID: %v", adminIDs)
	} else {
		log.Println("警告：未配置 ADMIN_IDS 环境变量")
	}

	var forwardToAdminID int64
	forwardToAdminIDStr := os.Getenv("FORWARD_TO_ADMIN_ID")
	if forwardToAdminIDStr != "" {
		forwardToAdminID, _ = strconv.ParseInt(forwardToAdminIDStr, 10, 64)
	}

	adminStates := make(map[int64]int)

	return &BotInstance{
		API:              api,
		adminIDs:         adminIDs,
		adminStates:      adminStates,
		forwardToAdminID: forwardToAdminID,
		redisClient:      redisClient,
		broadcastManager: broadcast.NewManager(api, redisClient, adminStates),
		welcomeManager:   welcome.NewManager(api, redisClient, adminStates),
	}, nil
}

// Run 函数保持不变
func (b *BotInstance) Run() {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := b.API.GetUpdatesChan(u)

	for update := range updates {
		b.handleUpdate(update)
	}
}

// handleUpdate 函数：新增存储用户信息的调用
func (b *BotInstance) handleUpdate(update tgbotapi.Update) {
	switch {
	case update.Message != nil:
		ctx := context.Background()
		// 存储用户的信息（用户名和昵称）
		if update.Message.From != nil {
			err := b.redisClient.StoreUserInfo(ctx, update.Message.From)
			if err != nil {
				log.Printf("存储用户 %d 信息失败: %v", update.Message.From.ID, err)
			}
		}
		// 仅当用户未被拉黑时才记录
		isBlocked, _ := b.redisClient.IsUserBlocked(ctx, update.Message.From.ID)
		if !isBlocked {
			b.redisClient.CheckAndAddUser(ctx, cache.UsersSetKey, update.Message.From.ID)
		}
		b.handleMessage(update.Message)
	case update.CallbackQuery != nil:
		b.handleCallbackQuery(update.CallbackQuery)
	}
}

// isAdmin 函数保持不变
func (b *BotInstance) isAdmin(userID int64) bool {
	return b.adminIDs[userID]
}

// handleMessage 函数保持不变
func (b *BotInstance) handleMessage(msg *tgbotapi.Message) {
	if b.isAdmin(msg.From.ID) {
		b.handleAdminMessage(msg)
	} else {
		b.handleUserMessage(msg)
	}
}

// handleAdminMessage 更新了管理员回复的逻辑
func (b *BotInstance) handleAdminMessage(msg *tgbotapi.Message) {
	if msg.ReplyToMessage != nil && b.forwardToAdminID == msg.Chat.ID {
		var originalUserID int64

		// 从被回复消息的文本或标题中解析用户ID
		var textToParse string
		if msg.ReplyToMessage.Text != "" {
			textToParse = msg.ReplyToMessage.Text
		} else if msg.ReplyToMessage.Caption != "" {
			textToParse = msg.ReplyToMessage.Caption
		}

		if textToParse != "" {
			re := regexp.MustCompile(`\((\d+)\)`)
			matches := re.FindStringSubmatch(textToParse)
			if len(matches) > 1 {
				id, err := strconv.ParseInt(matches[1], 10, 64)
				if err == nil {
					originalUserID = id
				}
			}
		}

		if originalUserID != 0 {
			var replyMsg tgbotapi.Chattable
			// 根据管理员回复的消息类型创建相应的消息
			if msg.Text != "" {
				replyMsg = tgbotapi.NewMessage(originalUserID, msg.Text)
			} else if msg.Sticker != nil {
				replyMsg = tgbotapi.NewSticker(originalUserID, tgbotapi.FileID(msg.Sticker.FileID))
			} else if len(msg.Photo) > 0 {
				photo := tgbotapi.NewPhoto(originalUserID, tgbotapi.FileID(msg.Photo[len(msg.Photo)-1].FileID))
				photo.Caption = msg.Caption
				replyMsg = photo
			} else if msg.Video != nil {
				video := tgbotapi.NewVideo(originalUserID, tgbotapi.FileID(msg.Video.FileID))
				video.Caption = msg.Caption
				replyMsg = video
			} else if msg.Document != nil {
				doc := tgbotapi.NewDocument(originalUserID, tgbotapi.FileID(msg.Document.FileID))
				doc.Caption = msg.Caption
				replyMsg = doc
			}

			if replyMsg != nil {
				_, err := b.API.Send(replyMsg)
				if err != nil {
					log.Printf("回复用户 %d 失败: %v", originalUserID, err)
					failMsg := tgbotapi.NewMessage(b.forwardToAdminID, fmt.Sprintf("❌ 回复用户 %d 失败。", originalUserID))
					b.API.Send(failMsg)
				} else {
					confirmMsg := tgbotapi.NewMessage(b.forwardToAdminID, "✅ 已回复给用户。")
					b.API.Send(confirmMsg)
				}
			} else {
				failMsg := tgbotapi.NewMessage(b.forwardToAdminID, "❌ 回复失败，不支持的消息类型。")
				b.API.Send(failMsg)
			}
		} else {
			failMsg := tgbotapi.NewMessage(b.forwardToAdminID, "❌ 回复失败，无法从此消息中解析到用户ID。")
			b.API.Send(failMsg)
		}
		return
	}

	// 处理管理员命令的逻辑
	if msg.IsCommand() {
		log.Printf("收到命令 %s 从 chatID %d", msg.Command(), msg.Chat.ID)
		switch msg.Command() {
		case "start":
			b.setCommandsForUser(msg.Chat.ID)
			b.welcomeManager.HandleStartCommand(msg.Chat.ID)
		case "setwelcome":
			b.welcomeManager.StartSetWelcomeProcess(msg.Chat.ID)
		case "setbuttons":
			b.welcomeManager.StartSetButtonsProcess(msg.Chat.ID)
		case "broadcast":
			b.broadcastManager.StartBroadcastBuilder(msg.Chat.ID)
		case "listblocked":
			b.handleListBlocked(msg.Chat.ID, 1)
		case "stats":
			b.handleUserStats(msg.Chat.ID)
		default:
			b.handleAdminStatefulMessage(msg)
		}
		return
	}

	b.handleAdminStatefulMessage(msg)
}

// handleListBlocked 函数：修改以显示用户名和昵称
func (b *BotInstance) handleListBlocked(chatID int64, page int) {
	ctx := context.Background()
	blockedIDs, err := b.redisClient.GetBlockedUserIDs(ctx)
	if err != nil {
		log.Printf("获取拉黑用户列表失败: %v", err)
		failMsg := tgbotapi.NewMessage(chatID, "❌ 获取拉黑用户列表失败。")
		b.API.Send(failMsg)
		return
	}

	if len(blockedIDs) == 0 {
		noBlockedMsg := tgbotapi.NewMessage(chatID, "当前没有拉黑的用户。")
		b.API.Send(noBlockedMsg)
		return
	}

	totalPages := (len(blockedIDs) + UsersPerPage - 1) / UsersPerPage
	if page < 1 || page > totalPages {
		page = 1
	}

	start := (page - 1) * UsersPerPage
	end := start + UsersPerPage
	if end > len(blockedIDs) {
		end = len(blockedIDs)
	}
	currentIDs := blockedIDs[start:end]

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("拉黑用户列表 (第 %d/%d 页):\n", page, totalPages))
	for i, idStr := range currentIDs {
		index := start + i + 1
		userID, _ := strconv.ParseInt(idStr, 10, 64)
		firstName, lastName, username, err := b.redisClient.GetUserInfo(ctx, userID)
		if err != nil {
			log.Printf("获取用户 %d 信息失败: %v", userID, err)
		}

		displayName := ""
		if username != "" {
			displayName = "@" + username
		}
		fullName := strings.TrimSpace(firstName + " " + lastName)
		if fullName != "" {
			if displayName != "" {
				displayName += " (" + fullName + ")"
			} else {
				displayName = fullName
			}
		}
		if displayName == "" {
			displayName = "Unknown"
		}
		displayName += " - ID: " + idStr
		sb.WriteString(fmt.Sprintf("%d. %s\n", index, displayName))
	}

	var keyboard [][]tgbotapi.InlineKeyboardButton
	for _, idStr := range currentIDs {
		userID, _ := strconv.ParseInt(idStr, 10, 64)
		firstName, lastName, username, _ := b.redisClient.GetUserInfo(ctx, userID)
		buttonText := "解除拉黑 " + idStr
		if username != "" {
			buttonText = "解除拉黑 @" + username + " (" + idStr + ")"
		} else if firstName != "" {
			buttonText = "解除拉黑 " + firstName + " " + lastName + " (" + idStr + ")"
		}
		unblockCallback := fmt.Sprintf("unblock_%s", idStr)
		unblockButton := tgbotapi.NewInlineKeyboardButtonData(buttonText, unblockCallback)
		keyboard = append(keyboard, tgbotapi.NewInlineKeyboardRow(unblockButton))
	}

	if totalPages > 1 {
		var paginationRow []tgbotapi.InlineKeyboardButton
		if page > 1 {
			paginationRow = append(paginationRow, tgbotapi.NewInlineKeyboardButtonData("上一页", fmt.Sprintf("page_prev_%d", page-1)))
		}
		if page < totalPages {
			paginationRow = append(paginationRow, tgbotapi.NewInlineKeyboardButtonData("下一页", fmt.Sprintf("page_next_%d", page+1)))
		}
		if len(paginationRow) > 0 {
			keyboard = append(keyboard, paginationRow)
		}
	}

	listMsg := tgbotapi.NewMessage(chatID, sb.String())
	if len(keyboard) > 0 {
		listMsg.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: keyboard}
	}
	b.API.Send(listMsg)
}

// handleUserStats 函数保持不变
func (b *BotInstance) handleUserStats(chatID int64) {
	ctx := context.Background()
	userIDs, err := b.redisClient.GetAllUserIDs(ctx, cache.UsersSetKey)
	if err != nil {
		log.Printf("获取用户统计失败: %v", err)
		failMsg := tgbotapi.NewMessage(chatID, "❌ 获取用户统计失败。")
		b.API.Send(failMsg)
		return
	}

	totalUsers := len(userIDs)
	blockedUsers, err := b.redisClient.GetBlockedUserIDs(ctx)
	if err != nil {
		log.Printf("获取拉黑用户统计失败: %v", err)
	}
	blockedCount := len(blockedUsers)
	activeUsers := totalUsers - blockedCount

	statsMsg := fmt.Sprintf("用户统计：\n- 总用户数: %d\n- 活跃用户数: %d\n- 拉黑用户数: %d", totalUsers, activeUsers, blockedCount)
	msg := tgbotapi.NewMessage(chatID, statsMsg)
	b.API.Send(msg)
}

// handleAdminStatefulMessage 修改以支持广播和欢迎消息处理
func (b *BotInstance) handleAdminStatefulMessage(msg *tgbotapi.Message) {
	log.Printf("处理管理员状态消息，chatID %d，当前状态: %d", msg.Chat.ID, b.adminStates[msg.Chat.ID])
	if b.welcomeManager.HandleAdminMessageInput(msg) {
		log.Printf("处理管理员消息（chatID %d）：已由 welcomeManager 处理", msg.Chat.ID)
		return
	}
	if b.broadcastManager.HandleMessageInput(msg) {
		log.Printf("处理管理员消息（chatID %d）：已由 broadcastManager 处理", msg.Chat.ID)
		return
	}
	log.Printf("未处理的管理员消息（chatID %d）：%v", msg.Chat.ID, msg.Text)
}

// handleCallbackQuery 函数保持不变
func (b *BotInstance) handleCallbackQuery(q *tgbotapi.CallbackQuery) {
	if strings.HasPrefix(q.Data, "unblock_") {
		parts := strings.Split(q.Data, "_")
		if len(parts) != 2 {
			return
		}
		userID, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return
		}

		err = b.redisClient.RemoveBlockedUser(context.Background(), userID)
		if err != nil {
			log.Printf("解除拉黑用户 %d 失败: %v", userID, err)
			return
		}

		callback := tgbotapi.NewCallback(q.ID, "✅ 用户已解除拉黑")
		b.API.Request(callback)
		currentPage := 1
		b.handleListBlocked(q.Message.Chat.ID, currentPage)
		return
	}

	if strings.HasPrefix(q.Data, "page_prev_") || strings.HasPrefix(q.Data, "page_next_") {
		parts := strings.Split(q.Data, "_")
		if len(parts) != 3 {
			return
		}
		newPage, err := strconv.Atoi(parts[2])
		if err != nil {
			return
		}
		b.handleListBlocked(q.Message.Chat.ID, newPage)
		b.API.Request(tgbotapi.NewCallback(q.ID, ""))
		return
	}

	if strings.HasPrefix(q.Data, "block_") {
		parts := strings.Split(q.Data, "_")
		if len(parts) != 2 {
			return
		}
		userID, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return
		}

		err = b.redisClient.AddBlockedUser(context.Background(), userID)
		if err != nil {
			log.Printf("拉黑用户 %d 失败: %v", userID, err)
			return
		}

		callback := tgbotapi.NewCallback(q.ID, "✅ 用户已拉黑")
		b.API.Request(callback)
		return
	}

	if b.broadcastManager.HandleCallbackQuery(q) {
		return
	}

	callback := tgbotapi.NewCallback(q.ID, "")
	b.API.Request(callback)
}

// escapeMarkdownV2 辅助函数保持不变
func escapeMarkdownV2(text string) string {
	reservedChars := []string{"_", "*", "[", "]", "(", ")", "~", "`", ">", "#", "+", "-", "=", "|", "{", "}", ".", "!"}
	for _, char := range reservedChars {
		text = strings.ReplaceAll(text, char, "\\"+char)
	}
	return text
}

// handleUserMessage 函数保持不变
func (b *BotInstance) handleUserMessage(msg *tgbotapi.Message) {
	isBlocked, err := b.redisClient.IsUserBlocked(context.Background(), msg.From.ID)
	if err != nil {
		log.Printf("检查用户 %d 是否被拉黑失败: %v", msg.From.ID, err)
		return
	}
	if isBlocked {
		blockedMsg := tgbotapi.NewMessage(msg.Chat.ID, "您已经被拉黑，暂时无法使用。")
		b.API.Send(blockedMsg)
		return
	}

	if msg.IsCommand() && msg.Command() == "start" {
		b.setCommandsForUser(msg.Chat.ID)
		b.welcomeManager.HandleStartCommand(msg.Chat.ID)
		return
	}

	if b.forwardToAdminID != 0 {
		escapedName := escapeMarkdownV2(msg.From.FirstName)
		caption := fmt.Sprintf("收到来自用户 [%s \\(%d\\)](tg://user?id=%d) 的消息:", escapedName, msg.From.ID, msg.From.ID)

		isBlocked, _ := b.redisClient.IsUserBlocked(context.Background(), msg.From.ID)
		var blockButton tgbotapi.InlineKeyboardButton
		if isBlocked {
			blockButton = tgbotapi.NewInlineKeyboardButtonData("解除拉黑", fmt.Sprintf("unblock_%d", msg.From.ID))
		} else {
			blockButton = tgbotapi.NewInlineKeyboardButtonData("拉黑用户", fmt.Sprintf("block_%d", msg.From.ID))
		}
		dialogButton := tgbotapi.NewInlineKeyboardButtonURL("与用户对话", fmt.Sprintf("tg://user?id=%d", msg.From.ID))
		keyboard := tgbotapi.NewInlineKeyboardMarkup(tgbotapi.NewInlineKeyboardRow(dialogButton, blockButton))

		var toAdminMsg tgbotapi.Chattable
		if msg.Text != "" {
			escapedText := escapeMarkdownV2(msg.Text)
			m := tgbotapi.NewMessage(b.forwardToAdminID, caption+"\n\n"+escapedText)
			m.ParseMode = "MarkdownV2"
			m.ReplyMarkup = keyboard
			toAdminMsg = m
		} else if len(msg.Photo) > 0 {
			p := tgbotapi.NewPhoto(b.forwardToAdminID, tgbotapi.FileID(msg.Photo[len(msg.Photo)-1].FileID))
			p.Caption = caption
			p.ParseMode = "MarkdownV2"
			p.ReplyMarkup = &keyboard
			toAdminMsg = p
		} else if msg.Sticker != nil {
			s := tgbotapi.NewSticker(b.forwardToAdminID, tgbotapi.FileID(msg.Sticker.FileID))
			b.API.Send(s)
			m := tgbotapi.NewMessage(b.forwardToAdminID, caption)
			m.ParseMode = "MarkdownV2"
			m.ReplyMarkup = keyboard
			toAdminMsg = m
		} else if msg.Video != nil {
			v := tgbotapi.NewVideo(b.forwardToAdminID, tgbotapi.FileID(msg.Video.FileID))
			v.Caption = caption
			v.ParseMode = "MarkdownV2"
			v.ReplyMarkup = &keyboard
			toAdminMsg = v
		} else if msg.Document != nil {
			d := tgbotapi.NewDocument(b.forwardToAdminID, tgbotapi.FileID(msg.Document.FileID))
			d.Caption = caption
			d.ParseMode = "MarkdownV2"
			d.ReplyMarkup = &keyboard
			toAdminMsg = d
		} else {
			m := tgbotapi.NewMessage(b.forwardToAdminID, caption+"\n\n[不支持的消息类型]")
			m.ParseMode = "MarkdownV2"
			m.ReplyMarkup = keyboard
			toAdminMsg = m
			log.Printf("用户 %d 发送了不支持的消息类型", msg.From.ID)
		}

		if toAdminMsg != nil {
			if _, err := b.API.Send(toAdminMsg); err != nil {
				log.Printf("发送消息副本给管理员失败: %v", err)
			}
		}

		reply := tgbotapi.NewMessage(msg.Chat.ID, "消息已收到，我们会尽快回复您。")
		b.API.Send(reply)
	} else {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "抱歉，当前无法处理您的消息。请稍后再试或联系管理员。")
		b.API.Send(reply)
		log.Printf("警告: 未配置 FORWARD_TO_ADMIN_ID，无法转发用户 %d 的消息", msg.From.ID)
	}
}

// setCommandsForUser 函数保持不变
func (b *BotInstance) setCommandsForUser(chatID int64) {
	var commands []tgbotapi.BotCommand

	if b.isAdmin(chatID) {
		commands = []tgbotapi.BotCommand{
			{Command: "start", Description: "查看欢迎信息"},
			{Command: "setwelcome", Description: "设置欢迎语"},
			{Command: "setbuttons", Description: "设置欢迎按钮"},
			{Command: "broadcast", Description: "创建广播"},
			{Command: "listblocked", Description: "查看拉黑用户列表"},
			{Command: "stats", Description: "查看用户统计"},
		}
	} else {
		commands = []tgbotapi.BotCommand{
			{Command: "start", Description: "获取欢迎信息"},
		}
	}

	config := tgbotapi.NewSetMyCommandsWithScope(tgbotapi.NewBotCommandScopeChat(chatID), commands...)
	_, err := b.API.Request(config)
	if err != nil {
		log.Printf("为用户 %d 设置命令失败: %v", chatID, err)
	}
}

// main 函数保持不变
func main() {
	bot, err := NewBotInstance()
	if err != nil {
		log.Fatalf("初始化机器人失败: %v", err)
	}

	log.Println("机器人已启动，正在等待消息...")
	bot.Run()
}
