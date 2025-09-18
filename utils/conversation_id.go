package utils

import (
	"crypto/md5"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
)

// ConversationIDManager 会话ID管理器 (SOLID-SRP: 单一职责)
type ConversationIDManager struct {
	cache map[string]string // 简单的内存缓存，生产环境可以使用Redis
}

// NewConversationIDManager 创建新的会话ID管理器
func NewConversationIDManager() *ConversationIDManager {
	return &ConversationIDManager{
		cache: make(map[string]string),
	}
}

// GenerateConversationID 基于客户端信息生成稳定的会话ID
// 遵循KISS原则：使用客户端特征生成稳定的标识符
func (c *ConversationIDManager) GenerateConversationID(ctx *gin.Context) string {
	// 从请求头中获取客户端标识信息
	clientIP := ctx.ClientIP()
	userAgent := ctx.GetHeader("User-Agent")

	// 检查是否有自定义的会话ID头（优先级最高）
	if customConvID := ctx.GetHeader("X-Conversation-ID"); customConvID != "" {
		return customConvID
	}

	// 为避免过于细粒度的会话分割，使用时间窗口来保持会话持久性
	// 每小时内的同一客户端使用相同的conversationId
	timeWindow := time.Now().Format("2006010215") // 精确到小时

	// 构建客户端特征字符串
	clientSignature := fmt.Sprintf("%s|%s|%s", clientIP, userAgent, timeWindow)

	// 检查缓存
	if cachedID, exists := c.cache[clientSignature]; exists {
		return cachedID
	}

	// 生成基于特征的MD5哈希
	hash := md5.Sum([]byte(clientSignature))
	conversationID := fmt.Sprintf("conv-%x", hash[:8]) // 使用前8字节，保持简洁

	// 缓存结果 (YAGNI: 简单内存缓存，未来可扩展为持久化)
	c.cache[clientSignature] = conversationID

	return conversationID
}

// GetOrCreateConversationID 获取或创建会话ID
func (c *ConversationIDManager) GetOrCreateConversationID(ctx *gin.Context) string {
	return c.GenerateConversationID(ctx)
}

// InvalidateOldSessions 清理过期的会话缓存
// SOLID-SRP: 单独的清理职责，避免内存泄漏
func (c *ConversationIDManager) InvalidateOldSessions() {
	// 简单实现：清空所有缓存，依赖时间窗口重新生成
	// 生产环境可以实现基于TTL的精确清理
	c.cache = make(map[string]string)
}

// 全局实例 - 单例模式 (SOLID-DIP: 提供抽象访问)
var globalConversationIDManager = NewConversationIDManager()

// GenerateStableConversationID 生成稳定的会话ID的全局函数
// 为了向后兼容和简化调用，提供全局访问函数
func GenerateStableConversationID(ctx *gin.Context) string {
	return globalConversationIDManager.GetOrCreateConversationID(ctx)
}

// GenerateStableAgentContinuationID 生成稳定的代理延续ID
// 复用现有的稳定ID生成机制，但使用不同的前缀以区分用途
func GenerateStableAgentContinuationID(ctx *gin.Context) string {
	// 从请求头中获取客户端标识信息
	clientIP := ctx.ClientIP()
	userAgent := ctx.GetHeader("User-Agent")

	// 检查是否有自定义的代理延续ID头（优先级最高）
	if customAgentID := ctx.GetHeader("X-Agent-Continuation-ID"); customAgentID != "" {
		return customAgentID
	}

	// 为了区分conversationId和agentContinuationId，使用更细粒度的时间窗口
	// 每10分钟内的同一客户端使用相同的agentContinuationId
	timeWindow := time.Now().Format("200601021504") // 精确到10分钟 (去掉最后一位)
	timeWindow = timeWindow[:len(timeWindow)-1]     // 截断到10分钟级别

	// 构建客户端特征字符串，加入agent前缀以区分用途
	clientSignature := fmt.Sprintf("agent|%s|%s|%s", clientIP, userAgent, timeWindow)

	// 生成基于特征的MD5哈希
	hash := md5.Sum([]byte(clientSignature))
	agentContinuationID := fmt.Sprintf("agent-%x", hash[:8]) // 使用前8字节，保持简洁

	return agentContinuationID
}

// ExtractClientInfo 提取客户端信息用于调试和日志
func ExtractClientInfo(ctx *gin.Context) map[string]string {
	return map[string]string{
		"client_ip":              ctx.ClientIP(),
		"user_agent":             ctx.GetHeader("User-Agent"),
		"custom_conv_id":         ctx.GetHeader("X-Conversation-ID"),
		"custom_agent_cont_id":   ctx.GetHeader("X-Agent-Continuation-ID"),
		"forwarded_for":          ctx.GetHeader("X-Forwarded-For"),
	}
}
